package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	flags "github.com/jessevdk/go-flags"
)

const (
	controlHost = "https://control.webscale.com"
	apiHost     = "https://api.webscale.com"
)

type Options struct {
	Verbose bool   `short:"v" long:"verbose" description:"Enable verbose output"`
	Log     LogCmd `command:"log" description:"Fetch CDN logs"`
}

type LogCmd struct {
	User   string `long:"user" required:"true" description:"Login email"`
	Pass   string `long:"pass" required:"true" description:"Login password"`
	Site   string `long:"site" required:"true" description:"Site domain name"`
	Filter string `long:"filter" description:"Log filter expression"`
	From   string `long:"from" description:"Start time (default: 30 days ago)" default:""`
	To     string `long:"to" description:"End time (default: now)" default:""`
	Type   string `long:"type" description:"Log type" default:"cdn"`
}

type sessionData struct {
	Token   string `json:"token"`
	Expiry  string `json:"expiry"`
	Account string `json:"account"`
}

func main() {
	var opts Options
	log.SetOutput(io.Discard)

	parser := flags.NewParser(&opts, flags.Default)
	globalOpts = &opts
	_, err := parser.Parse()
	if err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}
}

var globalOpts *Options

func (cmd *LogCmd) Execute(args []string) error {
	if globalOpts.Verbose {
		log.SetOutput(os.Stderr)
	}

	token, err := getOrCreateSession(cmd.User, cmd.Pass)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	appID, err := resolveApp(token, cmd.Site)
	if err != nil {
		return fmt.Errorf("resolving site: %w", err)
	}
	log.Printf("resolved %s to application %s", cmd.Site, appID)

	from := cmd.From
	to := cmd.To
	if from == "" {
		from = time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	}
	if to == "" {
		to = time.Now().UTC().Format(time.RFC3339)
	}

	return fetchLogs(token, appID, cmd.Filter, from, to, cmd.Type)
}

// --- Auth ---

func sessionPath() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "webscale", "session.json")
}

func loadSession() (*sessionData, error) {
	data, err := os.ReadFile(sessionPath())
	if err != nil {
		return nil, err
	}
	var s sessionData
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	exp, err := time.Parse(time.RFC3339Nano, s.Expiry)
	if err != nil {
		// try alternate format from API
		exp, err = time.Parse("2006-01-02T15:04:05.000000-07:00", s.Expiry)
		if err != nil {
			return nil, fmt.Errorf("parsing expiry: %w", err)
		}
	}
	if time.Now().After(exp.Add(-5 * time.Minute)) {
		return nil, fmt.Errorf("session expired")
	}
	return &s, nil
}

func saveSession(s *sessionData) error {
	p := sessionPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, _ := json.Marshal(s)
	return os.WriteFile(p, data, 0o600)
}

func getOrCreateSession(email, password string) (string, error) {
	if s, err := loadSession(); err == nil {
		log.Printf("using cached session (expires %s)", s.Expiry)
		return s.Token, nil
	}

	// Step 1: login
	log.Printf("logging in as %s", email)
	form := url.Values{
		"email":           {email},
		"password":        {password},
		"remember_device": {"true"},
	}
	resp, err := http.Post(controlHost+"/auth", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("login returned %d: %s", resp.StatusCode, body)
	}

	var loginResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return "", fmt.Errorf("decoding login response: %w", err)
	}

	// Step 2: get accounts
	accounts, err := apiGet[[]struct {
		ID   string `json:"id"`
		Href string `json:"href"`
	}](loginResp.Token, "/v2/accounts")
	if err != nil {
		return "", fmt.Errorf("fetching accounts: %w", err)
	}
	if len(*accounts) == 0 {
		return "", fmt.Errorf("no accounts found")
	}
	acct := (*accounts)[0]
	log.Printf("using account %s", acct.ID)

	// Step 3: authorize for account
	authBody, _ := json.Marshal(map[string]string{"account": acct.Href})
	req, _ := http.NewRequest("POST", apiHost+"/v2/users/self/authorization", strings.NewReader(string(authBody)))
	req.Header.Set("Authorization", "Bearer "+loginResp.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	authResp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer authResp.Body.Close() //nolint:errcheck

	if authResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(authResp.Body)
		return "", fmt.Errorf("authorization returned %d: %s", authResp.StatusCode, body)
	}

	var authResult struct {
		SecretKey string `json:"secret_key"`
		Expiry    string `json:"expiry"`
		Account   string `json:"account"`
	}
	if err := json.NewDecoder(authResp.Body).Decode(&authResult); err != nil {
		return "", fmt.Errorf("decoding authorization: %w", err)
	}

	sess := &sessionData{
		Token:   authResult.SecretKey,
		Expiry:  authResult.Expiry,
		Account: authResult.Account,
	}
	if err := saveSession(sess); err != nil {
		log.Printf("warning: could not cache session: %v", err)
	}

	return authResult.SecretKey, nil
}

// --- Application resolution ---

func resolveApp(token, domain string) (string, error) {
	type app struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Aliases []string `json:"aliases"`
	}

	apps, err := apiGet[[]app](token, "/v2/applications")
	if err != nil {
		return "", err
	}

	domain = strings.ToLower(domain)
	for _, a := range *apps {
		if strings.EqualFold(a.Name, domain) {
			return a.ID, nil
		}
		for _, alias := range a.Aliases {
			if strings.EqualFold(alias, domain) {
				return a.ID, nil
			}
		}
	}

	names := make([]string, len(*apps))
	for i, a := range *apps {
		names[i] = a.Name
	}
	return "", fmt.Errorf("site %q not found, available: %s", domain, strings.Join(names, ", "))
}

// --- Log fetching ---

func fetchLogs(token, appID, filter, from, to, logType string) error {
	params := url.Values{
		"authorization": {token},
		"from":          {from},
		"to":            {to},
		"type":          {logType},
		"format":        {"csv"},
	}
	if filter != "" {
		params.Set("filter", filter)
	}

	u := fmt.Sprintf("%s/v2/applications/%s/logs?%s", apiHost, appID,
		strings.ReplaceAll(params.Encode(), "+", "%20"))
	log.Printf("GET %s", u)

	resp, err := http.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("log download returned %d: %s", resp.StatusCode, body)
	}

	r := csv.NewReader(resp.Body)
	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("reading CSV header: %w", err)
	}

	fieldIdx := make(map[string]int, len(header))
	for i, f := range header {
		fieldIdx[f] = i
	}

	total := 0
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading CSV: %w", err)
		}

		get := func(name string) string {
			if i, ok := fieldIdx[name]; ok && i < len(row) && row[i] != "" {
				return row[i]
			}
			return "-"
		}

		ts := get("completed")
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			ts = t.Format("02/Jan/2006:15:04:05 -0700")
		}

		reqPath := get("request_path")
		if q := get("request_query"); q != "-" {
			reqPath += "?" + q
		}

		fmt.Fprintf(os.Stdout, "%s - - [%s] \"%s %s %s\" %s %s \"%s\" \"%s\"\n",
			get("request_address"),
			ts,
			get("request_method"),
			reqPath,
			get("protocol"),
			get("response_status_code"),
			get("bytes_out"),
			get("referrer"),
			get("useragent"),
		)
		total++
	}

	log.Printf("done, %d total records", total)
	return nil
}

// --- HTTP helpers ---

func apiGet[T any](token, path string) (*T, error) {
	u := apiHost + path
	log.Printf("GET %s", u)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API %s returned %d: %s", path, resp.StatusCode, body)
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	return &result, nil
}
