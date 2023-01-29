package ctc

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
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog/log"
)

type CTCCmd byte

const (
	CTCCmdTest    CTCCmd = 0x00
	CTCCmdWrite   CTCCmd = 0x01
	CTCCmdRead    CTCCmd = 0x02
	CTCCmdControl CTCCmd = 0x07
	CTCCmdSense   CTCCmd = 0x14
)

// HerculesVersion indicates which version of Hercules this CTC interface will
// connect to. Use HerculesVersionOld for Hercules 3.13, or HerculesVersionNew
// for Spinhawk and Hyperion.
type HerculesVersion int

const (
	// HerculesVersionOld is for use with Hercules 3.13.
	HerculesVersionOld HerculesVersion = iota

	// HerculesVersionNew if for use with Hercules Spinhawk and Hyperion.
	HerculesVersionNew
)

type CTC interface {
	Close()
	Connect() error
	Send(cmd CTCCmd, count uint16, data []byte) error
	Read() (cmd CTCCmd, count uint16, data []byte, err error)

	// ControlWrite will send a CONTROL, wait for the SENSE from the remote
	// side to clear the CONTROL, send the data with WRITE, and wait for the
	// READ from the remote side. Count for the WRITE will be the length of
	// the data.
	ControlWrite(data []byte) error

	NakedWrite(data []byte) error

	// SenseWait will await a SENSE, send a CONTROL in response, then perform
	// a READ, returning the bytes that were read.
	SenseRead() ([]byte, error)
}

// ErrAlreadyConnected is the error returned by Connect when at least half of
// the connection is already established. Call Close to reset the CTC
// connection before trying to connect again.
var ErrAlreadyConnected = errors.New("already connected")

// ErrInvalidVersion is the error returned by New when the version parameter
// is not HerculesVersionOld or HerculesVersionNew.
var ErrInvalidVersion = errors.New("invalid Hercules version")

// ErrNotConnected is the error returned when a send or receive operation is
// attempted on a CTC connection that is not connected.
var ErrNotConnected = errors.New("not connected")

type ctc struct {
	raddr              string
	rIP                net.IP
	rport              uint16
	lport              uint16
	recvsock, sendsock net.Conn
	seq                uint16
	devnum             uint16
	ver                HerculesVersion
	bo                 binary.ByteOrder
}

const ctcHdrLenOld = 12

type ctcHdrOld struct {
	CmdReg   CTCCmd
	FsmState byte
	SCount   uint16
	PktSeq   uint16
	SndLen   uint16
	DevNum   uint16
	SSID     uint16
}

const ctcHdrLenNew = 16

type ctcHdrNew struct {
	CmdReg   CTCCmd
	FsmState byte
	SCount   uint16
	PktSeq   uint16
	_        uint16
	SndLen   uint16
	DevNum   uint16
	SSID     uint16
	_        uint16
}

const ssid uint16 = 1

type ctcInitMsg struct {
	HercInfo  uint16
	LocalPort uint16
	RemoteIP  net.IP
	SndLen    uint16
	DevNum    uint16
	SSID      uint16
	_         uint16
}

func New(lport, rport, devnum uint16, raddr string, version HerculesVersion,
	byteOrder binary.ByteOrder) (CTC, error) {

	if !(version == HerculesVersionOld || version == HerculesVersionNew) {
		return nil, ErrInvalidVersion
	}

	rIP, err := net.ResolveIPAddr("ip", raddr)
	if err != nil {
		return nil, err
	}

	return &ctc{
		raddr:  raddr,
		rport:  rport,
		lport:  lport,
		rIP:    rIP.IP,
		seq:    1,
		devnum: devnum,
		ver:    version,
		bo:     byteOrder,
	}, nil
}

func (c *ctc) Close() {
	if c.sendsock != nil {
		log.Debug().Msg("Closing sendsock")
		c.sendsock.Close()
	}

	if c.recvsock != nil {
		log.Debug().Msg("Closing recvsock")
		c.recvsock.Close()
	}

	// Reset the ctc to its initial state
	c.sendsock = nil
	c.recvsock = nil
	c.seq = 1
}

func (c *ctc) Connect() error {
	if c.sendsock != nil || c.recvsock != nil {
		return ErrAlreadyConnected
	}

	// First, we wait for Hercules to connect to us. If the remote side is
	// Hercules 3.13, we listen on the odd port number and connect to the
	// odd port.
	lport := c.lport
	rport := c.rport
	if c.ver == HerculesVersionOld {
		lport++
		rport++
	}

	log.Info().Msgf("Waiting for Hercules to connect to us on port %d",
		lport)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", lport))
	if err != nil {
		return err
	}
	defer listener.Close()

	recvsock, err := listener.Accept()
	if err != nil {
		return err
	}
	log.Info().Msgf("Got connection from %s", recvsock.RemoteAddr().String())
	listener.Close()

	var sendsock net.Conn

	log.Info().Msgf("Connecting to remote hercules at %s:%d", c.raddr, rport)
	if c.ver == HerculesVersionNew {
		sendsock, err = net.Dial("tcp", fmt.Sprintf("%s:%d", c.raddr, rport))
		if err != nil {
			recvsock.Close()
			return err
		}
	} else {
		// Hercules 3.13 requires that we connect with a *source port* that
		// matches the remote port configured in its CTCE device.
		srcaddr, err := net.ResolveTCPAddr("tcp",
			fmt.Sprintf("0.0.0.0:%d", c.lport))
		if err != nil {
			recvsock.Close()
			return err
		}
		sendaddr, err := net.ResolveTCPAddr("tcp",
			fmt.Sprintf("%s:%d", c.raddr, rport))
		if err != nil {
			recvsock.Close()
			return err
		}
		sendsock, err = net.DialTCP("tcp", srcaddr, sendaddr)
		if err != nil {
			recvsock.Close()
			return err
		}
	}

	c.recvsock = recvsock
	c.sendsock = sendsock

	if c.ver == HerculesVersionNew {
		if err := c.handshake(); err != nil {
			recvsock.Close()
			sendsock.Close()
			c.recvsock = nil
			c.sendsock = nil
			return fmt.Errorf("handshake error: %v", err)
		}
		log.Info().Msg("Hercules handshake successful")
	}

	c.recvsock = recvsock
	c.sendsock = sendsock

	return nil
}

func (c *ctc) handshake() error {
	// Expect 16 bytes from Hercules, which we will simply discard.
	buf := make([]byte, ctcHdrLenNew)
	for n := 0; n < ctcHdrLenNew; {
		nn, err := c.recvsock.Read(buf[n:])
		if err != nil {
			return err
		}
		n += nn
	}

	// Now send our side of the handshake
	var sendbuf bytes.Buffer
	binary.Write(&sendbuf, c.bo, uint16(0x8010)) // "hercules info"
	binary.Write(&sendbuf, c.bo, c.lport)        // our listening port
	// our IP address, network byte order (big endian)
	if remoteip := c.rIP.To4(); remoteip != nil {
		sendbuf.Write(remoteip)
	} else {
		// I guess just send the first 4 bytes of the IPv6 address? Hercules
		// doesn't seem to handle anything but IPv4 in this field.
		sendbuf.Write(c.rIP[0:4])
	}
	binary.Write(&sendbuf, c.bo, uint16(ctcHdrLenNew)) // send length
	binary.Write(&sendbuf, c.bo, c.devnum)             // our device number
	binary.Write(&sendbuf, c.bo, ssid)                 // our ssid
	sendbuf.WriteByte(0)                               // padding
	sendbuf.WriteByte(0)                               // padding

	if _, err := c.sendsock.Write(sendbuf.Bytes()); err != nil {
		return err
	}

	return nil
}

func (c *ctc) Send(cmd CTCCmd, count uint16, data []byte) error {
	var buf bytes.Buffer

	if c.sendsock == nil || c.recvsock == nil {
		return ErrNotConnected
	}

	commandName := "unknown"
	var fsmState byte

	switch cmd {
	case CTCCmdControl:
		commandName = "CONTROL"
		fsmState = 0x01
	case CTCCmdRead:
		commandName = "READ"
		fsmState = 0x04
	case CTCCmdSense:
		commandName = "SENSE"
		fsmState = 0x04
	case CTCCmdWrite:
		commandName = "WRITE"
		fsmState = 0x03
	}

	if c.ver == HerculesVersionOld {
		binary.Write(&buf, c.bo, ctcHdrOld{
			CmdReg:   cmd,
			FsmState: fsmState,
			SCount:   count,
			PktSeq:   c.seq,
			SndLen:   ctcHdrLenOld + uint16(len(data)),
			DevNum:   c.devnum,
			SSID:     ssid,
		})
	} else {
		binary.Write(&buf, c.bo, ctcHdrNew{
			CmdReg:   cmd,
			FsmState: fsmState,
			SCount:   count,
			PktSeq:   c.seq,
			SndLen:   ctcHdrLenNew + uint16(len(data)),
			DevNum:   c.devnum,
			SSID:     ssid,
		})
	}

	buf.Write(data)

	log.Trace().Str("command", commandName).Hex("data", buf.Bytes()).Msg("SEND")

	if _, err := c.sendsock.Write(buf.Bytes()); err != nil {
		return err
	}

	c.seq++
	return nil
}

func (c *ctc) Read() (cmd CTCCmd, count uint16, data []byte, err error) {
	for {
		cmd, count, data, err = c.read()
		if err != nil {
			return cmd, count, data, err
		}

		if cmd != CTCCmdTest {
			return cmd, count, data, nil
		}

		// If we got a test I/O command, discard it and wait for next command.
	}
}

func (c *ctc) read() (cmd CTCCmd, count uint16, data []byte, err error) {
	var buf []byte
	if c.ver == HerculesVersionOld {
		buf = make([]byte, ctcHdrLenOld)
	} else {
		buf = make([]byte, ctcHdrLenNew)
	}

	// Read the header info
	for n := 0; n < len(buf); {
		nn, err := c.recvsock.Read(buf[n:])
		if err != nil {
			return 0, 0, nil, err
		}
		n += nn
	}

	log.Trace().Hex("header", buf).Msg("READ")

	var dataLen uint16

	if c.ver == HerculesVersionOld {
		var header ctcHdrOld
		if err := binary.Read(bytes.NewBuffer(buf), c.bo, &header); err != nil {
			return 0, 0, nil, err
		}

		cmd = header.CmdReg
		count = header.SCount
		dataLen = header.SndLen - ctcHdrLenOld
	} else {
		var header ctcHdrNew
		if err := binary.Read(bytes.NewBuffer(buf), c.bo, &header); err != nil {
			return 0, 0, nil, err
		}

		cmd = header.CmdReg
		count = header.SCount
		dataLen = header.SndLen - ctcHdrLenNew
	}

	// Now read the rest of the data, if any.
	if dataLen > 0 {
		data = make([]byte, dataLen)
		for n := 0; n < len(data); {
			nn, err := c.recvsock.Read(data[n:])
			if err != nil {
				return cmd, count, data, err
			}
			n += nn
		}
	} else {
		data = make([]byte, 0)
	}

	log.Trace().Hex("data", data).Msg("READ")

	return cmd, count, data, nil
}

// ControlWrite will send a CONTROL, wait for the SENSE from the remote side
// to clear the CONTROL, send the data with WRITE, and wait for the READ from
// the remote side. Count for the WRITE will be the length of the data.
func (c *ctc) ControlWrite(data []byte) error {
	log.Debug().Msg("ctc.ControlWrite(): sending CONTROL")
	if err := c.Send(CTCCmdControl, 1, nil); err != nil {
		return fmt.Errorf("couldn't send CONTROL: %v", err)
	}

	// Expect a SENSE command in response.
	log.Debug().Msg("ctc.ControlWrite(): awaiting SENSE")
	cmd, _, _, err := c.Read()
	if err != nil {
		return fmt.Errorf("couldn't read while awaiting SENSE: %v", err)
	}
	if cmd != CTCCmdSense {
		return fmt.Errorf("expected SENSE but got %02x", cmd)
	}

	// Putting this brief pause between doing the control/sense then the
	// write seems to eliminate an intermittent condition during stress
	// testing on my system where we get the sense from Hercules/MVS, but
	// then either Hercules or MVS never picks up the write state change.
	time.Sleep(10 * time.Millisecond)
	log.Debug().Msg("ctc.ControlWrite: sending WRITE")
	if err := c.Send(CTCCmdWrite, uint16(len(data)), data); err != nil {
		return fmt.Errorf("couldn't send WRITE: %v", err)
	}

	// Expect the corresponding READ command from the other side
	log.Debug().Msg("ctc.ControlWrite(): awaiting READ")
	cmd, _, _, err = c.Read()
	if err != nil {
		return fmt.Errorf("couldn't read while awaiting READ: %v", err)
	}
	if cmd != CTCCmdRead {
		return fmt.Errorf("expected READ, but got %02x", cmd)
	}

	return nil
}

// NakedWrite will send the data with WRITE, and wait for the READ from
// the remote side. Count for the WRITE will be the length of the data.
func (c *ctc) NakedWrite(data []byte) error {

	log.Debug().Msg("ctc.NakedWrite: sending WRITE")
	if err := c.Send(CTCCmdWrite, uint16(len(data)), data); err != nil {
		return fmt.Errorf("couldn't send WRITE: %v", err)
	}

	// Expect the corresponding READ command from the other side
	log.Debug().Msg("ctc.ConNakedWritetrolWrite(): awaiting READ")
	cmd, _, _, err := c.Read()
	if err != nil {
		return fmt.Errorf("couldn't read while awaiting READ: %v", err)
	}
	if cmd != CTCCmdRead {
		return fmt.Errorf("expected READ, but got %02x", cmd)
	}

	return nil
}

// SenseWait will await a SENSE, send a CONTROL in response, then perform a
// READ, returning the bytes that were read.
func (c *ctc) SenseRead() ([]byte, error) {
	log.Debug().Msg("ctc.SenseRead(): awaiting CONTROL")
	cmd, _, _, err := c.Read()
	if err != nil {
		return nil, fmt.Errorf("couldn't read while awaiting CONTROL: %v", err)
	}
	if cmd != CTCCmdControl {
		return nil, fmt.Errorf("expected CONTROL, but got %02x", cmd)
	}

	// Send a SENSE command in response
	log.Debug().Msg("ctc.SenseRead(): sending SENSE")
	if err := c.Send(CTCCmdSense, 1, nil); err != nil {
		return nil, fmt.Errorf("couldn't send SENSE: %v", err)
	}

	// Read the data
	log.Debug().Msg("ctc.SenseRead(): reading data")
	cmd, count, data, err := c.Read()
	if err != nil {
		return nil, fmt.Errorf("couldn't read data: %v", err)
	}
	log.Debug().
		Hex("command", []byte{byte(cmd)}).
		Uint16("count", count).
		Hex("data", data).
		Msg("data read from CTC adapter")
	if cmd != CTCCmdWrite {
		return data, fmt.Errorf("expected WRITE, but got %02x", cmd)
	}
	// Now send our READ command to indicate we've read the response
	if err := c.Send(CTCCmdRead, count, nil); err != nil {
		return data, fmt.Errorf("couldn't send READ in respone to WRITE")
	}

	return data, nil
}
