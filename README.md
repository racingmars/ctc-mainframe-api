# CTC Mainframe API

An HTTP web service for your MVS 3.8 mainframe, via an emulated
channel-to-channel adapter, with support for all versions of Hercules in
common use (3.13, Spinhawk, and SDL-Hyperion).

No guarantees are made as to functionality or reliability. Additionally, see
the "Limitations and security" section of this document for important security
information.

By Matthew R. Wilson, <mwilson@mattwilson.org>. Original repository at
<https://github.com/racingmars/ctc-mainframe-api/>

## Repository layout

This repository contains the following subdirectories for each component of
the overall product:

**ctcserver**: Go program that communicates with the MVS service over the
emulated CTC adapter and presents the functions as a web service API.

**MVS**: the members of a partitioned dataset in MVS with the assembler source
for the MVS-side service and various JCL procedures to build and run the
service.

## Available functions

### Dataset list

`GET /api/dslist/<prefix>`

The dataset list will search the catalog for all datasets that begin with
`<prefix>` and return basic information about them. If the prefix is a single
component (e.g. `FOO` instead of `FOO.BAR`), the API will add a period
(`FOO.`) so that actual datasets are returned beginning with `FOO.` instead of
just the `FOO` alias entry in the catalog.

### PDS member list

`GET /api/mbrlist/<pds>`

If `<pds>` is a partitioned dataset, the member list API will return the list
of member names.

### Read dataset

`GET /api/read/<dsn>`

`<dsn>` is the name of a dataset you wish to read. The response body will be
of type text/plain containing the ASCII-converted records with trailing spaces
trimmed and a newline inserted after each record.

Sequential datasets (e.g. `HLQ.DS1`) and members of partitioned datasets (e.g.
`HLQ.DS2(MEMBER)`) are supported. However, only datasets with fixed record
length (F or FB) are supported.

### Quit

`GET /api/quit`

Calling this API will stop the job running the CTC service on the MVS side. To
prevent CTC device syncronization problems, you should not make further API
calls to the web service until the CTC server job is started on the MVS side
again.

## Example API usage

The combination of the _PDF member list_ API and the _Read dataset_ API allow
you to easily save all the members from a PDS to a local directory. For
example, with the API service running on port 8370, I wish to retrieve all
members of the partitioned dataset `MWILSON.CTCSERV` into a new directory to
backup the source code for this project:

```
$ mkdir CTCSERV
$ cd CTCSERV
$ for x in $(curl -s http://127.0.0.1:8370/api/mbrlist/mwilson.ctcserv | jq -r '.[]')
for>  do
for>    curl -s -o "$x" "http://127.0.0.1:8370/api/read/mwilson.ctcserv($x)"
for>  done
$ ls
'$$$INDEX'  '$BUILD'  '$COPYING'  '$DEBUG'  '$RUN'   CTCSERV   DSLIST   MBRLIST
READ
```

## Limitations and security

**Security: there is none**. No security is implemented at all on either the
web service side or the MVS service side. Anyone who has access to the web
service, or directly to the emulated CTC device ports on your Hercules
instance, will be able to make full use of the services.

I have not tested this on an MVS system with RAKF (or, for that matter, RACF)
installed. A security product may limit the actions the service can take to
those that the user running the service can take. If this is important to you,
you would need to thoroughly test that assumption. Future updates to the
MVS-side code may require that it run APF-authorized; at that point, even your
security product may not apply its access controls to operations performed
through this service. _Caveat emptor_.

A _non-exhaustive_ list of current known limitations includes:

 - All access to datasets assumes that they are cataloged; there is no support
   for specifying volumes to access uncataloged names.
 - Any actions involving datasets that span multiple volumes are untested.

The **only** public interface is the HTTP API provided by the Go server; the
CTC interface on the mainframe side is intended only for use by the
accompanying Go code. It does not do any input validation; it is programmed to
assume the calling Go code already takes care of this.

## License

Copyright 2022 Matthew R. Wilson <mwilson@mattwilson.org>.

This program is free software: you can redistribute it and/or modify it under
the terms of the GNU General Public License as published by the Free Software
Foundation, either version 3 of the License, or (at your option) any later
version.

This program is distributed in the hope that it will be useful, but WITHOUT
ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS
FOR A PARTICULAR PURPOSE. See the GNU General Public License for more details.

You should have received a copy of the GNU General Public License along with
this program. If not, see <https://www.gnu.org/licenses/>.
