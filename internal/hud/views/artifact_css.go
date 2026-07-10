package views

import _ "embed"

//go:embed artifact.css
var ArtifactCSS string

// CSPMeta is the Content-Security-Policy meta tag every artifact HTML document
// carries (ringer.py:82-85): no external anything, inline styles, data: images.
const CSPMeta = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:">`
