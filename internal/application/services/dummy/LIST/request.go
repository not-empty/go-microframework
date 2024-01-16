package dummy

type Request struct {
	Data *Data
}

type Data struct {
	Page int `validate:"required"`
}

func NewRequest(data *Data) Request {
	return Request{
		Data: data,
	}
}

func (r *Request) Validate() error {
	return nil
}
