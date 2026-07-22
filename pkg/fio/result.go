package fio

import "encoding/json"

type FIOResult struct {
	Jobs []FIOJobResult `json:"jobs"`
}

type FIOJobResult struct {
	Jobname string       `json:"jobname"`
	Error   int          `json:"error"`
	Read    FIODirection `json:"read"`
	Write   FIODirection `json:"write"`
}

type FIODirection struct {
	IOPS    float64 `json:"iops"`
	BWBytes int64   `json:"bw_bytes"`
	LatNS   FIOLat  `json:"lat_ns"`
}

type FIOLat struct {
	Mean float64 `json:"mean"`
	Max  float64 `json:"max"`
}

func ParseResult(data []byte) (*FIOResult, error) {
	var r FIOResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
