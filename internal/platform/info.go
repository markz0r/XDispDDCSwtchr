package platform

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"runtime"
)

type Info struct {
	OS           string `json:"name"`
	Version      string `json:"version"`
	Build        string `json:"build"`
	Architecture string `json:"architecture"`
}

func Current() (Info, error) {
	info := Info{OS: runtime.GOOS, Architecture: runtime.GOARCH}
	if runtime.GOOS != "darwin" {
		return info, nil
	}
	file, err := os.Open("/System/Library/CoreServices/SystemVersion.plist")
	if err != nil {
		return Info{}, fmt.Errorf("read macOS system version: %w", err)
	}
	defer file.Close()
	values, err := parsePlistDictionary(file)
	if err != nil {
		return Info{}, fmt.Errorf("parse macOS system version: %w", err)
	}
	info.Version = values["ProductVersion"]
	info.Build = values["ProductBuildVersion"]
	if info.Version == "" || info.Build == "" {
		return Info{}, fmt.Errorf("macOS system version plist lacks ProductVersion or ProductBuildVersion")
	}
	return info, nil
}

func parsePlistDictionary(reader io.Reader) (map[string]string, error) {
	decoder := xml.NewDecoder(reader)
	values := make(map[string]string)
	var key string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return values, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "key":
			if err := decoder.DecodeElement(&key, &start); err != nil {
				return nil, err
			}
		case "string":
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				return nil, err
			}
			if key != "" {
				values[key] = value
				key = ""
			}
		}
	}
}
