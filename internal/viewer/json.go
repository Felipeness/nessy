package viewer

import "encoding/json"

// Wrappers locais pra manter o código de ledger.go limpo.
func jsonUnmarshal(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}

func jsonMarshalIndent(v any, indent string) ([]byte, error) {
	return json.MarshalIndent(v, "", indent)
}
