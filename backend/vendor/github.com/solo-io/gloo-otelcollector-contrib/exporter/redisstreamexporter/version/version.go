package version

import _ "embed"

// Version to use when storing into redis
//
//go:embed version.txt
var Version string
