package core

import "strings"

func NormalizeValue(stateValue, desiredValue string) string {
	if strings.EqualFold(stateValue, desiredValue) {
		return stateValue
	}
	return desiredValue
}
