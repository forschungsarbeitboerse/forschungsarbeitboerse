package main

import (
	"errors"
	"net/mail"
	"regexp"
)

var ErrUnknownEmail = errors.New("E-Mail ist nicht auf der Liste der zulässigen Adressen bzw. Einrichtungen.")

func validateMailAddress(validRegexp []*regexp.Regexp, email string) error {
	if _, err := mail.ParseAddress(email); err != nil {
		// The given address is not a valid email address
		return err
	}

	if len(validRegexp) == 0 {
		// Nothing to do
		return nil
	}

	for _, r := range validRegexp {
		if r.Match([]byte(email)) {
			// Mail address does match one of the valid patterns
			// i.e. is accepted without admin verification
			return nil
		}
	}

	return ErrUnknownEmail
}
