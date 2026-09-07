package auto_remediation

// Logger defines logging interface
type Logger interface {
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
}

// DefaultLogger is a simple no-op logger
type DefaultLogger struct{}

func (DefaultLogger) Infof(format string, args ...interface{})   {}
func (DefaultLogger) Warnf(format string, args ...interface{})   {}
func (DefaultLogger) Errorf(format string, args ...interface{})  {}
func (DefaultLogger) Debugf(format string, args ...interface{})  {}

// Use this in place of actual logger during development
var logger Logger = DefaultLogger{}
