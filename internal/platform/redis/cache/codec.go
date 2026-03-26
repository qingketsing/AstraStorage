package cache

import "encoding/json"

func Encode(value any) ([]byte, error) {
	return json.Marshal(value)
}

func Decode(payload []byte, target any) error {
	return json.Unmarshal(payload, target)
}
