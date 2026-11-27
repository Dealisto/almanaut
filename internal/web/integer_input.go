package web

import (
	"net/http"
	"strconv"
)

type integerInput struct{ Type, Value string }

// integerInputValue keeps rejected POST bodies separate from the parsed item:
// parsing errors can leave an integer field at zero, which is not the draft.
// GET queries never override stored values or new-form defaults.
func integerInputValue(req *http.Request, errorText, name string, value int) integerInput {
	input := integerInput{Type: "number", Value: strconv.Itoa(value)}
	if req != nil && req.Method == http.MethodPost && errorText != "" {
		input.Value = req.PostFormValue(name)
		if numberNeedsTextInput(input.Value) {
			input.Type = "text"
		}
	}
	return input
}
