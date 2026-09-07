package ocr

import _ "embed"

//go:embed testdata/fonts/LiberationSans-Regular.ttf
var liberationSansFile []byte

func liberationSansTTF() ([]byte, error) {
	if len(liberationSansFile) == 0 {
		return nil, errEmptyFont
	}
	return liberationSansFile, nil
}
