package util

import (
	"strings"

	"github.com/google/uuid"
)

func GetUUID() string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	return code
}
