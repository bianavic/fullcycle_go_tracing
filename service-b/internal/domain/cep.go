package domain

// cepLength is the number of digits in a Brazilian CEP.
const cepLength = 8

type CEP struct {
	value string
}

func NewCEP(s string) (CEP, error) {
	if len(s) != cepLength {
		return CEP{}, ErrInvalidZipcode
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return CEP{}, ErrInvalidZipcode
		}
	}
	return CEP{value: s}, nil
}

func (c CEP) String() string { return c.value }
