package cmd

// ExitCodeError is returned when a command needs to exit with a specific code.
// main.go checks for this error type and calls os.Exit(Code).
type ExitCodeError struct {
	Code    int
	Message string
}

func (e *ExitCodeError) Error() string { return e.Message }
