package log

// Nop returns a Logger that discards all output. Use in unit tests.
func Nop() Logger {
	return nopLogger{}
}

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}
func (nopLogger) With(...any) Logger   { return nopLogger{} }
