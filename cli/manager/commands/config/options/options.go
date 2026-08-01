package options

type Action string

const (
	Interactive Action = "interactive"
	List        Action = "list"
	Get         Action = "get"
	Set         Action = "set"
	Reset       Action = "reset"
)

type Request struct {
	Action       Action
	Key          string
	Value        string
	Experimental bool
	All          bool
}

type Environment interface {
	Configure(Request)
}
