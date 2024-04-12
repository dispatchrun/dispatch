package cli

import "fmt"

type authError struct{}

func (authError) Error() string {
	const message = "Authentication error when contacting the Dispatch API"
	var detail string
	switch DispatchApiKeyLocation {
	case "env":
		detail = "check DISPATCH_API_KEY environment variable"
	case "cli":
		detail = "check the -k,--api-key command-line option"
	default:
		detail = "please login again using: dispatch login"
	}
	return fmt.Sprintf("%s (%s)", message, detail)
}
