package log

// Noop returns a Logger that discards all output. Use in unit tests.
func Noop() Logger {
	return noopLogger{}
}

type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}
func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}
func (noopLogger) With(...any) Logger   { return noopLogger{} }
