package log

//go:generate mockgen -source=logger.go -destination=mock_log/mock_logger.go -package=mock_log

type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	With(args ...any) Logger
}
