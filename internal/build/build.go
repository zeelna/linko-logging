package build

// default build-time variables
var (
	GitSHA    = "unknown"
	BuildTime = "unknown"
)

// At build time, inject the real values with -ldflags:
// go build -ldflags "-X my/package/build.GitSHA=$(git rev-parse HEAD) -X my/package/build.BuildTime=$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

// IMPORTANT: go run does not support -ldflags variable injection – the variables will remain "unknown" unless you use go build first.

/* // #1 Build your app using -ldflags to inject values at link time:
go build \
  -ldflags "-X github.com/zeelna/linko-logging/internal/build.GitSHA=$(git rev-parse HEAD) -X github.com/zeelna/linko-logging/internal/build.BuildTime=$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
  -o linko

//  #2 Run the prebuilt app with the log file path set:
LINKO_LOG_FILE=linko.access.log ./linko
*/
