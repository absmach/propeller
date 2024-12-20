package task

import (
	"encoding/json"
	"net/url"
)

type URLValue url.URL

func (u URLValue) MarshalJSON() ([]byte, error) {
	return json.Marshal((*url.URL)(&u).String())
}

func (u *URLValue) UnmarshalJSON(data []byte) error {
	var urlStr string
	if err := json.Unmarshal(data, &urlStr); err != nil {
		return err
	}

	if urlStr == "" {
		return nil
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return err
	}

	*u = URLValue(*parsedURL)

	return nil
}

func (u *URLValue) URL() *url.URL {
	if u == nil {
		return nil
	}
	val := url.URL(*u)

	return &val
}

func (u URLValue) String() string {
	return (*url.URL)(&u).String()
}
