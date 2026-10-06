package fault

type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string          { return e.Code }
func New(status int, code string) error { return &Error{Status: status, Code: code} }
