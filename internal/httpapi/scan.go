package httpapi

import "net/http"

func scanLibrary(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Scanner == nil {
			writeError(w, http.StatusServiceUnavailable, "SCANNER_UNAVAILABLE", "library scanner is unavailable")
			return
		}
		result, err := deps.Scanner.Scan(r.Context(), r.PathValue("libraryId"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "SCAN_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
