//go:build moflexenc

package media

import _ "embed"

// The live moflex encoder for this platform, built into Big Top (see encoder.go). build.sh copies it
// into encoder/ from the moflex-encoder build and builds with -tags moflexenc.
//
//go:embed encoder/linux_amd64/moflex-ffmpeg
var embeddedEncoder []byte
