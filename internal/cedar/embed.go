package cedar

import "embed"

//go:embed schema/hcp.cedarschema
var Schema []byte

//go:embed templates
var templatesFS embed.FS
