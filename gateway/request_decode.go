package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

var errRequestBodyTooLarge = errors.New("request body too large")

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	reader := http.MaxBytesReader(w, r.Body, configMaxBodySize)
	return decodeJSONReader(reader, dst)
}

func decodeJSONBodyDisallowUnknownFields(w http.ResponseWriter, r *http.Request, dst any) error {
	reader := http.MaxBytesReader(w, r.Body, configMaxBodySize)
	return decodeJSONReaderWithOptions(reader, dst, true)
}

func decodeOptionalJSONBody(w http.ResponseWriter, r *http.Request, dst any) (bool, error) {
	return decodeOptionalJSONBodyWithOptions(w, r, dst, false)
}

func decodeOptionalJSONBodyDisallowUnknownFields(w http.ResponseWriter, r *http.Request, dst any) (bool, error) {
	return decodeOptionalJSONBodyWithOptions(w, r, dst, true)
}

func decodeOptionalJSONBodyWithOptions(w http.ResponseWriter, r *http.Request, dst any, disallowUnknownFields bool) (bool, error) {
	if r == nil || r.Body == nil {
		return false, nil
	}
	reader := http.MaxBytesReader(w, r.Body, configMaxBodySize)
	body, err := io.ReadAll(reader)
	if err != nil {
		if isRequestBodyTooLarge(err) {
			return false, errRequestBodyTooLarge
		}
		return false, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return false, nil
	}
	if err := decodeJSONReaderWithOptions(bytes.NewReader(body), dst, disallowUnknownFields); err != nil {
		return false, err
	}
	return true, nil
}

func decodeJSONReader(reader io.Reader, dst any) error {
	return decodeJSONReaderWithOptions(reader, dst, false)
}

func decodeJSONReaderWithOptions(reader io.Reader, dst any, disallowUnknownFields bool) error {
	dec := json.NewDecoder(reader)
	if disallowUnknownFields {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(dst); err != nil {
		if isRequestBodyTooLarge(err) {
			return errRequestBodyTooLarge
		}
		return err
	}

	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		if isRequestBodyTooLarge(err) {
			return errRequestBodyTooLarge
		}
		if err == nil {
			return errors.New("unexpected trailing data")
		}
		return err
	}
	return nil
}

func isRequestBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}
