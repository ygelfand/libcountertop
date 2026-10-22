//go:build payload

package firmware

import _ "embed"

//go:embed tessera-armv7
var Binary []byte
