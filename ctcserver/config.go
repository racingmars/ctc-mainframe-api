package main

// Copyright 2022 Matthew R. Wilson <mwilson@mattwilson.org>
//
// This file is part of CTC Mainframe API. CTC Mainframe API is free software:
// you can redistribute it and/or modify it under the terms of the GNU General
// Public License as published by the Free Software Foundation, either version
// 3 of the license, or (at your option) any later version.
//
// https://github.com/racingmars/ctc-mainframe-api/

import (
	"encoding/json"
	"fmt"
	"os"
)

type configuration struct {
	ListenPort            uint16 `json:"listen_port"`
	HerculesHost          string `json:"hercules_host"`
	Hercules313           bool   `json:"hercules_v313"`
	HerculesHostBigEndian bool   `json:"hercules_host_bigendian"`
	CmdLPort              uint16 `json:"cmd_local_port"`
	CmdRPort              uint16 `json:"cmd_remote_port"`
	DataLPort             uint16 `json:"data_local_port"`
	DataRPort             uint16 `json:"data_remote_port"`
}

func readConfig(path string) (configuration, error) {
	var c configuration

	f, err := os.Open(path)
	if err != nil {
		return c, fmt.Errorf("couldn't open config file '%s': %v", path, err)
	}
	defer f.Close()

	decoder := json.NewDecoder(f)
	if err := decoder.Decode(&c); err != nil {
		return c, fmt.Errorf("couldn't decode config JSON: %v", err)
	}

	return c, nil
}
