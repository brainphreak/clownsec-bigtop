// Package web holds Big Top's setup page, built into the program.
package web

import _ "embed"

//go:embed index.html
var Index []byte

// HLS is hls.js 1.5.20 (Apache-2.0, https://github.com/video-dev/hls.js): the browser player's HLS
// for browsers without it built in (all but Safari).
//
//go:embed hls.min.js
var HLS []byte
