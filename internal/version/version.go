package version

import (
	"fmt"
	"runtime"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// UserAgent returns the User-Agent string for HTTP requests.
func UserAgent() string {
	return fmt.Sprintf(
		"provider-mirror/%s (%s/%s; +https://github.com/petroprotsakh/go-provider-mirror)",
		Version,
		runtime.GOOS,
		runtime.GOARCH,
	)
}
