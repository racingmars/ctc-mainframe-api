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
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/racingmars/ctc-mainframe-api/ctcserver/internal/ctc"
)

type DSInfo struct {
	Type      string
	Name      string
	Volume    string
	DSOrg     string
	RecFM     string
	BlockSize int
	LRecLen   int
}

var dsprefixRegex = regexp.MustCompile(
	`^[a-zA-Z$#@-][a-zA-Z0-9$#@-]{0,7}` +
		`(\.[a-zA-Z$#@-][a-zA-Z0-9$#@-]{0,7})*\.?$`)

var dsnameRegex = regexp.MustCompile(
	`^[a-zA-Z$#@-][a-zA-Z0-9$#@-]{0,7}` +
		`(\.[a-zA-Z$#@-][a-zA-Z0-9$#@-]{0,7})*$`)

var dsnameOptionalMemberRegex = regexp.MustCompile(
	`^([a-zA-Z$#@-][a-zA-Z0-9$#@-]{0,7}` +
		`(?:\.[a-zA-Z$#@-][a-zA-Z0-9$#@-]{0,7})*)` +
		`(?:\(([a-zA-Z$#@-][a-zA-Z0-9$#@-]{1,8})\))?$`)

func (c *ctcapi) GetDSList(basename string) ([]DSInfo, error) {
	if len(basename) > 44 {
		return nil, fmt.Errorf("dataset name too long; got %d characters "+
			"but needs to be 44 or fewer", len(basename))
	}

	if !dsprefixRegex.MatchString(basename) {
		return nil, fmt.Errorf("dataset search prefix is invalid")
	}

	// Always treat a bare HLQ as a complete, specific HLQ and add a period to
	// the end of it. This will cause the catalog search to return all of the
	// datasets under that HLQ instead of just returning a single result with
	// the alias entry in the master catalog for the HLQ.
	if !strings.Contains(basename, ".") {
		basename += "."
	}

	basenameEbcdic := ctc.StoE(strings.ToUpper(basename))

	log.Debug().Hex("ebcdic", basenameEbcdic).Msgf(
		"GetDSList(): performing catalog search for '%s'", basename)

	c.ctcMutex.Lock()
	defer c.ctcMutex.Unlock()

	if err := c.sendCommand(opDSList, basenameEbcdic); err != nil {
		log.Error().Err(err).Send()
		return nil, err
	}

	// Wait for a CONTROL command
	cmd, _, _, err := c.ctcdata.Read()
	if err != nil {
		log.Error().Err(err).Msg(
			"GetDSList(): error during ctcdata.Read() awaiting CONTROL")
		return nil, err
	}
	if cmd != ctc.CTCCmdControl {
		log.Error().Msgf("GetDSList(): didn't receive expected CONTROL "+
			"command during ctcdata.Read(), got %02x", cmd)
		return nil, fmt.Errorf("received unexpected CTC command")
	}

	// Send a SENSE command in response
	if err := c.ctcdata.Send(ctc.CTCCmdSense, 1, nil); err != nil {
		log.Error().Err(err).Msg(
			"GetDSList(): error trying to read SENSE from ctcdata")
		return nil, err
	}

	// Read the initial command response. This is two fullwords: response code
	// and list length.
	cmd, count, data, err := c.ctcdata.Read()
	if err != nil {
		log.Error().Err(err).Msg(
			"GetDSList(): error trying to read initial response")
		return nil, err
	}
	log.Debug().
		Hex("command", []byte{byte(cmd)}).
		Uint16("count", count).
		Hex("data", data).
		Msg("GetDSList() initial response")

	// Now send our READ command to indicate we've read the response
	if err := c.ctcdata.Send(ctc.CTCCmdRead, count, nil); err != nil {
		log.Error().Err(err).Msg(
			"GetDSList(): error sending read after initial response")
		return nil, err
	}

	resultCode := binary.BigEndian.Uint32(data[0:4])
	numEntries := binary.BigEndian.Uint16(data[4:6])
	if resultCode != 0 {
		log.Info().Msgf("GetDSList(): unsuccessful result code: %02x",
			resultCode)
		return nil, fmt.Errorf("unsuccessful catalog search result code: %02x",
			resultCode)
	}

	log.Debug().Msgf("GetDSList(): mumber of results: %d", numEntries)

	var entries []DSInfo
	for i := 0; i < int(numEntries); i++ {
		// Read the entry
		cmd, count, data, err := c.ctcdata.Read()
		if err != nil {
			log.Error().Err(err).Msgf("GetDSList(): error reading entry %d", i)
			return nil, err
		}
		log.Debug().
			Hex("command", []byte{byte(cmd)}).
			Uint16("count", count).
			Hex("data", data).
			Msgf("GetDSList(): got entry %d", i)
		// Now send our READ command to indicate we've read the response
		if err := c.ctcdata.Send(ctc.CTCCmdRead, count, nil); err != nil {
			log.Error().Err(err).Msgf(
				"GetDSList(): error sending read after reading entry %d", i)
			return nil, err
		}

		var dsinfo DSInfo
		dsinfo.Type = ctc.EtoS(data[0:1])
		dsinfo.Name = strings.TrimSpace(ctc.EtoS(data[1:45]))
		dsinfo.Volume = strings.TrimSpace(ctc.EtoS(data[45:51]))

		// data[] starting at index 51 corresponds to the 96 bytes of a
		// (likely) format-1 DSCB beginning at offset 44/0x2C (that is, the 96
		// bytes returned as part of the OBTAIN macro).

		if data[51] != 0xF1 && dsinfo.Type != "X" {
			log.Warn().Msgf("GetDSLIST(): unexpected DSCB format type: "+
				"expecting F1, but got %02x for %s", data[51], dsinfo.Name)
		}

		// For DSORG bit definitions, see DS1DSORG in SYS1.AMODGEN(IECSDSL1)
		switch {
		case data[89]&0x80 > 0:
			dsinfo.DSOrg = "IS"
		case data[89]&0x40 > 0:
			dsinfo.DSOrg = "PS"
		case data[89]&0x20 > 0:
			dsinfo.DSOrg = "DA"
		case data[89]&0x10 > 0:
			dsinfo.DSOrg = "CX"
		case data[89]&0x02 > 0:
			dsinfo.DSOrg = "PO"
		case data[90]&0x08 > 0: // note 2nd byte of DS1DSORG
			dsinfo.DSOrg = "VS"
		default:
			dsinfo.DSOrg = "Unk"
		}

		switch {
		case data[91]&0xC0 == 0x80:
			dsinfo.RecFM = "F"
		case data[91]&0xC0 == 0x40:
			dsinfo.RecFM = "V"
		case data[91]&0xC0 == 0xC0:
			dsinfo.RecFM = "U"
		}

		// Additionally, we can add a "B" for blocked
		if data[91]&0x10 == 0x10 {
			dsinfo.RecFM += "B"
		}

		// And if it's variable, it can be spanned
		if data[91]&0xC0 == 0x40 && data[91]&0x08 == 0x08 {
			dsinfo.RecFM += "S"
		}

		dsinfo.BlockSize = int(binary.BigEndian.Uint16(data[93:95]))
		dsinfo.LRecLen = int(binary.BigEndian.Uint16(data[95:97]))

		entries = append(entries, dsinfo)
	}

	return entries, nil
}

func (c *ctcapi) GetMemberList(pdsName string) ([]string, error) {
	if len(pdsName) > 44 {
		return nil, fmt.Errorf("dataset name too long; got %d characters "+
			"but needs to be 44 or fewer", len(pdsName))
	}

	if !dsnameRegex.MatchString(pdsName) {
		return nil, fmt.Errorf("dataset name is invalid")
	}

	// The dataset name to MBRLIST must be 44 characters, padded with (EBCDIC)
	// spaces.
	pdsEbcdic := ctc.StoE(strings.ToUpper(pdsName))
	pdsPadded := make([]byte, 44)
	for i := range pdsPadded {
		pdsPadded[i] = 0x40
	}
	copy(pdsPadded, pdsEbcdic)

	c.ctcMutex.Lock()
	defer c.ctcMutex.Unlock()

	log.Debug().Hex("pds", pdsEbcdic).Msgf("getting member list for '%s'",
		pdsName)

	if err := c.sendCommand(opMbrList, pdsPadded); err != nil {
		log.Error().Err(err).Msg("sendCommand() error in GetMemberList()")
		return nil, err
	}

	// Wait for a CONTROL command
	cmd, _, _, err := c.ctcdata.Read()
	if err != nil {
		log.Error().Err(err).Msg("error during ctcdata.Read() while " +
			"awaiting CONTROL in GetMemberList()")
		return nil, err
	}
	if cmd != ctc.CTCCmdControl {
		errmsg := fmt.Sprintf("didn't receive expected CONTROL command "+
			"during ctcdata.Read(), got %02x", cmd)
		log.Error().Msg(errmsg)
		return nil, errors.New(errmsg)
	}

	// Send a SENSE command in response
	if err := c.ctcdata.Send(ctc.CTCCmdSense, 1, nil); err != nil {
		log.Error().Err(err).Msg(
			"error trying to read SENSE from ctcdata in GetMemberList()")
		return nil, err
	}

	// Read the initial response
	cmd, count, data, err := c.ctcdata.Read()
	if err != nil {
		log.Info().Err(err).Msgf(
			"error during ctcdata.Read() in GetMemberList()")
		return nil, err
	}
	log.Debug().
		Hex("command", []byte{byte(cmd)}).
		Uint16("count", count).
		Hex("data", data).
		Msg("GetMemberList(): initial response")

	// Now send our READ command to indicate we've read the response
	if err := c.ctcdata.Send(ctc.CTCCmdRead, count, nil); err != nil {
		log.Info().Err(err).Msgf(
			"GetMemberList(): error sending READ after initial response")
		return nil, err
	}

	resultCode := binary.BigEndian.Uint32(data[0:4])
	if resultCode != 0 {
		additionalCode := binary.BigEndian.Uint32(data[4:8])
		log.Info().Msgf("GetMemberList(): unsuccessful result code: %02x/%02x",
			resultCode, additionalCode)
		return nil, fmt.Errorf("unsuccessful result code: %02x/%02x",
			resultCode, additionalCode)
	}

	var entries []string
	for {
		// Read the entry
		cmd, count, data, err := c.ctcdata.Read()
		if err != nil {
			log.Info().Err(err).Msg("GetMemberList(): error reading entry")
			return nil, err
		}
		log.Debug().
			Hex("command", []byte{byte(cmd)}).
			Uint16("count", count).
			Hex("data", data).
			Msg("GetMemberList(): read entry")
		// Now send our READ command to indicate we've read the response
		if err := c.ctcdata.Send(ctc.CTCCmdRead, count, nil); err != nil {
			log.Error().Err(err).Msg(
				"GetMemberList(): error sending READ after reading entry")
			return nil, err
		}

		if data[0] == 0xFF && data[1] == 0xFF && data[2] == 0xFF &&
			data[3] == 0xFF && data[4] == 0xFF && data[5] == 0xFF &&
			data[6] == 0xFF && data[7] == 0xFF {
			// last member entry all high bytes. Done
			break
		}

		name := strings.TrimRight(ctc.EtoS(data[0:8]), " ")
		entries = append(entries, name)
	}

	return entries, nil
}

func (c *ctcapi) Read(dsn string) ([]string, error) {

	if !dsnameOptionalMemberRegex.MatchString(dsn) {
		return nil, fmt.Errorf("dataset name is invalid")
	}

	matches := dsnameOptionalMemberRegex.FindStringSubmatch(dsn)
	pdsName := matches[1]
	mbrName := matches[2]

	if len(pdsName) > 44 {
		return nil, fmt.Errorf("dataset name too long; got %d characters "+
			"but needs to be 44 or fewer", len(pdsName))
	}
	if len(mbrName) > 8 {
		return nil, fmt.Errorf("member name too long; got %d characters "+
			"but needs to be 8 or fewer", len(mbrName))
	}

	// The dataset name must be 44 characters, padded with (EBCDIC) spaces.
	pdsEbcdic := ctc.StoE(strings.ToUpper(pdsName))
	pdsPadded := make([]byte, 44)
	for i := range pdsPadded {
		pdsPadded[i] = 0x40
	}
	copy(pdsPadded, pdsEbcdic)

	// The member name must be 8 characters, padded with (EBCDIC) spaces.
	mbrEbcdic := ctc.StoE(strings.ToUpper(mbrName))
	mbrPadded := make([]byte, 8)
	for i := range mbrPadded {
		mbrPadded[i] = 0x40
	}
	copy(mbrPadded, mbrEbcdic)

	c.ctcMutex.Lock()
	defer c.ctcMutex.Unlock()

	log.Debug().Hex("pds", pdsEbcdic).Msgf("reading dataset '%s'",
		pdsName)
	if len(mbrName) > 0 {
		log.Debug().Hex("member", mbrEbcdic).Msgf("reading member '%s'",
			mbrName)
	}

	// Complete input is the 44-byte DS name followed by 8-byte member
	pdsPadded = append(pdsPadded, mbrPadded...)

	if err := c.sendCommand(opRead, pdsPadded); err != nil {
		log.Error().Err(err).Msg("sendCommand() error in ReadDS()")
		return nil, err
	}

	// Wait for a CONTROL command
	cmd, _, _, err := c.ctcdata.Read()
	if err != nil {
		log.Error().Err(err).Msg("error during ctcdata.Read() while " +
			"awaiting CONTROL in ReadDS()")
		return nil, err
	}
	if cmd != ctc.CTCCmdControl {
		errmsg := fmt.Sprintf("didn't receive expected CONTROL command "+
			"during ctcdata.Read(), got %02x", cmd)
		log.Error().Msg(errmsg)
		return nil, errors.New(errmsg)
	}

	// Send a SENSE command in response
	if err := c.ctcdata.Send(ctc.CTCCmdSense, 1, nil); err != nil {
		log.Error().Err(err).Msg(
			"error trying to read SENSE from ctcdata in ReadDS()")
		return nil, err
	}

	// Read the initial response
	cmd, count, data, err := c.ctcdata.Read()
	if err != nil {
		log.Info().Err(err).Msgf(
			"error during ctcdata.Read() in ReadDS()")
		return nil, err
	}
	log.Debug().
		Hex("command", []byte{byte(cmd)}).
		Uint16("count", count).
		Hex("data", data).
		Msg("ReadDS(): initial response")

	// Now send our READ command to indicate we've read the response
	if err := c.ctcdata.Send(ctc.CTCCmdRead, count, nil); err != nil {
		log.Info().Err(err).Msgf(
			"ReadDS(): error sending READ after initial response")
		return nil, err
	}

	resultCode := binary.BigEndian.Uint32(data[0:4])
	if resultCode != 0 {
		additionalCode := binary.BigEndian.Uint32(data[4:8])
		log.Info().Msgf("ReadDS(): unsuccessful result code: %02x/%02x",
			resultCode, additionalCode)
		return nil, fmt.Errorf("unsuccessful result code: %02x/%02x",
			resultCode, additionalCode)
	}

	var entries []string
	for {
		// Read the entry
		cmd, count, data, err := c.ctcdata.Read()
		if err != nil {
			log.Info().Err(err).Msg("ReadDS(): error reading entry")
			return nil, err
		}
		log.Debug().
			Hex("command", []byte{byte(cmd)}).
			Uint16("count", count).
			Hex("data", data).
			Msg("ReadDS(): read entry")
		// Now send our READ command to indicate we've read the response
		if err := c.ctcdata.Send(ctc.CTCCmdRead, count, nil); err != nil {
			log.Error().Err(err).Msg(
				"ReadDS(): error sending READ after reading entry")
			return nil, err
		}

		if len(data) == 1 && data[0] == 0xFF {
			// last record. Done
			break
		}

		record := strings.TrimRight(ctc.EtoS(data), " ")
		entries = append(entries, record)
	}

	return entries, nil
}

// Quit will instruct the CTC server job on the MVS side to quit.
func (c *ctcapi) Quit() error {
	c.ctcMutex.Lock()
	defer c.ctcMutex.Unlock()

	log.Debug().Msg("sending quit command")

	if err := c.sendCommand(opQuit, nil); err != nil {
		log.Error().Err(err).Msgf("Quit(): error sending quit command")
		return err
	}

	return nil
}
