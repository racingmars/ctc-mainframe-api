# CTC Mainframe API

An HTTP web service for your MVS 3.8 mainframe, via an emulated
channel-to-channel adapter, with support for all versions of Hercules in
common use (3.13, Spinhawk, and SDL-Hyperion).

No guarantees are made as to functionality or reliability. Additionally, see
the "Limitations and security" section of this document for important security
information.

By Matthew R. Wilson, <mwilson@mattwilson.org>. Original repository at
<https://github.com/racingmars/ctc-mainframe-api/>

**This is very preliminary and everything is subject to change.**

## How-To

### Configure Hercules

All Hercules versions and forks from Hercules 3.13 onward are supported.
However, the CTC implementation in Hercules 3.13 CTC is much less robust than
in Spinhawk and Hyperion in terms of initial connection order of operations
and connection retry capability.

Add a pair of CTC adapters to your Hercules configuration file. The following
syntax works in Hercules 3.13, Spinhawk, and Hyperion.

```
#         lport rhost     rport
0502 CTCE 15620 127.0.0.1 15600
0503 CTCE 15630 127.0.0.1 15610
```

If using Hercules 3.13, lport and rport all must be even numbers. rhost is the
address of the system you're running the Go binary on that will host the HTTP
API.

The device numbers (502 and 503 in this example) must go into the JCL
procedure that starts the CTCSERV program in MVS in the `CTCCMD` and `CTCDATA`
DD statements. The devices must have been defined as CTC devices in your
system. 500-503 are available for use in the default Moseley sysgen.

### Configure ctcserver

Next, copy the config.json.sample file to config.json and adjust it
appropriately:

 * `listen_port` is the HTTP listener port the service will listen on.
 * `hercules_host` is the address of your system Hercules runs on.
 * `hercules_v313` must be true for Hercules 3.13, false for all other
   versions (spinhawk, hyperion).
 * `hercules_host_bigendian` should be false for most users. If your Hercules
   is running on a big endian system (sparcv9, ppc64be, s390x, etc.), set to
   true.
 * `cmd_local_port` should match the lport of your first CTC definition in
   Hercules (15620 in the above example).
 * `cmd_remote_port` should match the rport of your first CTC definition in
   Hercules (15600 in the above example).
 * `data_local_port` should match the lport of your second CTC definition in
   Hercules (15630 in the above example).
 * `data_remote_port` should match the lport of your second CTC definition in
   Hercules (15610 in the above example).

### Start everything

**If you're using Hercules 3.13**, startup order is very important:

 1. Start the ctcserver binary on your host system.
 2. Start Hercules on your host system.
 3. IPL MVS.

For spinhawk and hyperion, startup order doesn't matter. Those versions of
Hercules will always attempt to re-establish the CTC connections, so you can
start and stop the ctcserver binary without needing to shut down and restart
Hercules and MVS.

Once ctcserver is running and MVS is IPLed, start the CTCSERV job under MVS.
You may now make HTTP requests against the API.

The available functions are listed in the "Available functions" section of
this document.

### Recovering from problems

The CTC adapters are very sensitive to maintaing correct state synchronization
between all parties involved. Furthermore, any bugs in my code could also
contribute to things getting out of a good state. If you're using Hercules
3.13... you probably just need to shut everything down and start over.

But if you're using Spinhawk or Hyperion and things stop working, you can
recover without needing to re-IPL MVS:

 1. Make sure the CTCSERV job on MVS is stopped (e.g. cancel it from the
    console if you have to).
 2. Take the CTC adapters offline from the MVS console (e.g. `V 502,OFFLINE`
    and `V 503,OFFLINE`).
 3. Remove the CTC adapters from Hercules (e.g. `detach 502` and
    `detach 503`).
 4. Re-add the CTC adapters to Hercules (e.g.
    `attach 502 CTCE 15620 127.0.0.1 15600` and
    `attach 503 CTCE 15630 127.0.0.1 15610`).
 5. Vary the CTC adapters online from the MVS console (e.g. `V 502,ONLINE` and
    `V 503,ONLINE`).
 6. Start the CTCSERV job in MVS again, and start the ctcserver binary on the
    host system again.

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

Alternatively, for the raw EBCDIC version of the data, add an `ebcdic=true`
query parameter: `GET /api/read/<dsn>?ebcdic=true`. This will return a content
type of application/octet-stream with the data from the mainframe left
untouched.

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

The combination of the _PDS member list_ API and the _Read dataset_ API allow
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

## TODO

One problem with the implementation right now is that I haven't yet figured
out how to wait on data to arrive at the CTC adapter, so I have to sit in a
polling loop running a SENSE CCW. That's not ideal. There may be a way to get
MVS to act on the real device attention interruption and POST to a WAIT in the
service... the first attempt I've made at this didn't work, but there's still
something else to try.

Otherwise, it'd be cool to add:

 * Job submission (relatively straightforward: I can DYNALLOC an internal
   reader and write JCL records to it).
 * Writing, instead of just reading, datasets.
 * Get job status and job output (as far as I can tell from some other
   software on MVS 3.8, the only way to do this is to read the SYS1.HASPCKPT
   dataset directly...I've not found any documentation for the format of the
   data in there yet, though).
 * Could probably add functions to list online volumes and some other MVS
   status information.
 * Could support uncataloged datasets when a volume name is provided and
   listing VTOCs for a volume instead of just a catalog search.

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
