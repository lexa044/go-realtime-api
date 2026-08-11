package security

import "golang.org/x/crypto/bcrypt"

// BcryptHasher implements usecase.PasswordHasher using bcrypt at the
// package's default cost. The cost is deliberately not configurable per
// call — a fixed cost keeps every stored hash comparable and stops a
// caller from accidentally choosing a weak one.
type BcryptHasher struct{}

func NewBcryptHasher() BcryptHasher { return BcryptHasher{} }

func (BcryptHasher) Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (BcryptHasher) Compare(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
