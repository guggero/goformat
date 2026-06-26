package nolintfunc

//nolint:lll
func DoNotFormat(someVeryLongArgumentName int, anotherLongOne int, andYetAnotherLongOne int) error {
	if someVeryLongArgumentName > 0 && anotherLongOne > 0 && andYetAnotherLongOne > 0 {
		return nil
	}
	return nil
}

// nolint
func AlsoUntouched(items []string) string {
	return items[0] + items[1] + items[2] + items[3] + items[4] + items[5] + items[6]
}

// This one IS formatted.
func DoFormat(someVeryLongArgumentName int, anotherLongOne int, andYetAnotherLongOne int) error {
	return nil
}
