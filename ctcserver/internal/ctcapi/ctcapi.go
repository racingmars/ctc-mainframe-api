package ctcapi

// Copyright 2022-2023 Matthew R. Wilson <mwilson@mattwilson.org>
//
// This file is part of CTC Mainframe API. CTC Mainframe API is free software:
// you can redistribute it and/or modify it under the terms of the GNU General
// Public License as published by the Free Software Foundation, either version
// 3 of the license, or (at your option) any later version.
//
// https://github.com/racingmars/ctc-mainframe-api/

import (
	"bytes"
	"encoding/binary"
	"sync"

	"github.com/racingmars/ctc-mainframe-api/ctcserver/internal/ctc"
	"github.com/rs/zerolog/log"
)

type CTCAPI interface {
	GetDSList(basename string) ([]DSInfo, error)
	GetMemberList(pdsName string) ([]string, error)
	Read(dsn string, raw bool) ([][]byte, error)
	Write(dsn string, data []string) error
	Submit(jcl []string) (string, error)
	Quit() error
}

type ctcapi struct {
	ctccmd, ctcdata ctc.CTC
	ctcMutex        sync.Mutex
}

type opcode byte

const (
	opDSList  opcode = 0x01
	opMbrList opcode = 0x02
	opRead    opcode = 0x03
	opSubmit  opcode = 0x04
	opWrite   opcode = 0x05
	opQuit    opcode = 0xFF
)

func New(ctccmd, ctcdata ctc.CTC) CTCAPI {
	c := ctcapi{
		ctccmd:  ctccmd,
		ctcdata: ctcdata,
	}

	return &c
}

func (c *ctcapi) sendCommand(op opcode, param []byte) error {
	// Build the command buffer -- pad the parameter to 255 length w/ EBCDIC
	// spaces
	var buf bytes.Buffer
	buf.WriteByte(byte(op))
	binary.Write(&buf, binary.BigEndian, uint16(len(param)))
	parampadded := make([]byte, 255)
	copy(parampadded, param)
	buf.Write(parampadded)

	// Send it with a CONTROL+WRITE
	log.Debug().Msgf("Sending opcode %02x with param %x")
	if err := c.ctccmd.ControlWrite(buf.Bytes()); err != nil {
		return err
	}

	return nil
}
