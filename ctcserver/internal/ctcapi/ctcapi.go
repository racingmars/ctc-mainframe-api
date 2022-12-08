package ctcapi

// Copyright 2022 Matthew R. Wilson <mwilson@mattwilson.org>
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

	"github.com/rs/zerolog/log"

	"github.com/racingmars/ctc-mainframe-api/ctcserver/internal/ctc"
)

type CTCAPI interface {
	GetDSList(basename string) ([]DSInfo, error)
	GetMemberList(pdsName string) ([]string, error)
	Read(dsn string) ([]string, error)
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
	// Send CONTROL to get the server's attention.
	if err := c.ctccmd.Send(ctc.CTCCmdControl, 1, nil); err != nil {
		return err
	}

	// Expect a SENSE command in response.
	cmd, _, _, err := c.ctccmd.Read()
	if err != nil {
		return err
	}
	if cmd != ctc.CTCCmdSense {
		log.Error().Msgf("expected SENSE but got %02x.", cmd)
	}

	// Send the WRITE with the command line
	var buf bytes.Buffer
	buf.WriteByte(byte(op))
	binary.Write(&buf, binary.BigEndian, uint16(len(param)))
	parampadded := make([]byte, 255)
	copy(parampadded, param)
	buf.Write(parampadded)

	if err := c.ctccmd.Send(ctc.CTCCmdWrite, uint16(buf.Len()),
		buf.Bytes()); err != nil {
		return err
	}

	// And now expect a READ command in response
	cmd, _, _, err = c.ctccmd.Read()
	if err != nil {
		return err
	}
	if cmd != ctc.CTCCmdRead {
		log.Error().Msgf("expected READ but got %02x", cmd)
	}

	return nil
}
