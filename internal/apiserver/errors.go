/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package apiserver

import (
	"encoding/json"
	"net/http"

	"github.com/apahim/hyperkube/internal/desires"
)

func writeError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError

	switch {
	case desires.IsNotFoundError(err):
		code = http.StatusNotFound
	case desires.IsAlreadyExistsError(err):
		code = http.StatusConflict
	case desires.IsPreconditionFailedError(err):
		code = http.StatusConflict
	}

	writeErrorMsg(w, code, err.Error())
}

func writeErrorMsg(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": message,
		"code":  code,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
