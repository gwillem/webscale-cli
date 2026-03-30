# webscale-cli

CLI tool to fetch CDN logs from the Webscale platform, output in Apache Combined Log Format.

## Install

```
go install github.com/gwillem/webscale-cli@latest
```

## Usage

```
webscale-cli log --user you@example.com --pass yourpassword --site example.com
```

### Options

```
webscale-cli log [options]

      --user=    Login email
      --pass=    Login password
      --site=    Site domain name
      --filter=  Log filter expression
      --from=    Start time (default: 30 days ago)
      --to=      End time (default: now)
      --type=    Log type (default: cdn)
  -v, --verbose  Enable verbose output
```

### Examples

```bash
# Fetch all CDN logs for a site
webscale-cli log --user you@example.com --pass secret --site example.com

# Filter by request path
webscale-cli log --user you@example.com --pass secret --site example.com \
  --filter 'request_path ~ "*admin*"'

# Custom time range
webscale-cli log --user you@example.com --pass secret --site example.com \
  --from 2026-03-01T00:00:00Z --to 2026-03-15T00:00:00Z
```

Session tokens are cached in `$XDG_CACHE_HOME/webscale/` (defaults to `~/.cache/webscale/`) to avoid re-authentication on subsequent runs.
