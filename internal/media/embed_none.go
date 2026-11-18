//go:build !moflexenc || !((darwin && arm64) || (darwin && amd64) || (linux && amd64) || (windows && amd64))

package media

// No encoder built in (a plain development build, or a platform without one): Big Top then looks
// for one installed beside it or in its data folder (MoflexEncoder).
var embeddedEncoder []byte
