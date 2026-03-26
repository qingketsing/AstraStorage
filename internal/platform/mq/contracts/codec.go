package contracts

import "encoding/json"

func EncodeEnvelope(envelope Envelope) ([]byte, error) {
	return json.Marshal(envelope)
}

func DecodeEnvelope(body []byte, target *Envelope) error {
	return json.Unmarshal(body, target)
}

func MustPayload(payload any) json.RawMessage {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return raw
}
