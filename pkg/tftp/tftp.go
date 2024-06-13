// Copyright 2012-2024 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tftp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"pack.ag/tftp"
)

type Flags struct {
	Cmd       string
	Mode      string
	PortRange string
	Literal   bool
	Verbose   bool
}

type clientCfg struct {
	host    string
	port    string
	client  ClientIf
	mode    tftp.TransferMode
	rexmt   tftp.ClientOpt
	timeout tftp.ClientOpt
	trace   bool
	literal bool
	verbose bool
}

func RunInteractive(f Flags, ipPort []string, stdin io.Reader, stdout io.Writer) error {
	const defaultPort = "69"
	var ipHost string
	var port string
	inScan := bufio.NewScanner(stdin)

	if len(ipPort) == 0 {
		ipHost = readHostInteractive(inScan, stdout)
	} else {
		ipHost = ipPort[0]

		if len(ipPort) > 1 {
			port = ipPort[1]
		} else {
			port = defaultPort
		}
	}

	clientcfg := &clientCfg{
		host:    ipHost,
		port:    port,
		mode:    tftp.ModeNetASCII,
		rexmt:   tftp.ClientRetransmit(10),
		timeout: tftp.ClientTimeout(1),
		trace:   false,
		literal: f.Literal,
	}

	for {
		input := readInputInteractive(inScan, stdout)
		exit, err := executeOp(input, clientcfg, stdout)
		if err != nil {
			fmt.Fprintf(stdout, "%v", err)
		}
		if exit {
			return nil
		}
	}
}

func executeOp(input []string, clientcfg *clientCfg, stdout io.Writer) (bool, error) {
	var err error

	switch input[0] {
	case "q", "quit":
		return true, nil
	case "h", "help", "?":
		fmt.Fprintf(stdout, "%s", printHelp())
	case "ascii":
		clientcfg.mode, _ = validateMode("ascii")
	case "binary":
		clientcfg.mode, _ = validateMode("binary")
	case "mode":
		if len(input) > 1 {
			clientcfg.mode, err = validateMode(input[1])
			if err != nil {
				fmt.Fprintf(stdout, "%v", err)

			}
		}
		fmt.Fprintf(stdout, "Using %s mode to transfer files.\n", clientcfg.mode)
	case "get":
		clientcfg.client, err = NewClient(clientcfg)
		if err != nil {
			return false, err
		}

		err = executeGet(clientcfg.client, clientcfg.host, clientcfg.port, input[1:])
	case "put":
		clientcfg.client, err = NewClient(clientcfg)
		if err != nil {
			return false, err
		}

		err = executePut(clientcfg.client, clientcfg.host, clientcfg.port, input[1:])
	case "connect":
		if len(input) > 2 {
			clientcfg.port = input[2]
		}
		clientcfg.host = input[1]
	case "literal":
		clientcfg.literal = !clientcfg.literal
		fmt.Fprintf(stdout, "Literal mode is %s\n", statusString(clientcfg.literal))
	case "rexmt":
		var val int
		val, err = strconv.Atoi(input[1])

		clientcfg.rexmt = tftp.ClientRetransmit(val)
	case "status":
		fmt.Fprintf(stdout, "Connected to %s\n", clientcfg.host)
		fmt.Fprintf(stdout, "Mode: %s Verbose: %s Tracing: %s Literal: %s\n",
			clientcfg.mode,
			statusString(clientcfg.verbose),
			statusString(clientcfg.trace),
			statusString(clientcfg.literal),
		)
	case "timeout":
		var val int
		val, err = strconv.Atoi(input[1])

		clientcfg.timeout = tftp.ClientTimeout(val)
	case "trace":
		clientcfg.trace = !clientcfg.trace
		fmt.Fprintf(stdout, "Packet tracing %s.\n", statusString(clientcfg.trace))
	case "verbose":
		clientcfg.verbose = !clientcfg.verbose
		fmt.Fprintf(stdout, "Verbose mode %s.\n", statusString(clientcfg.verbose))
	}
	if err != nil {
		fmt.Fprintf(stdout, "%v\n", err)
	}
	return false, nil
}

func constructURL(host, port, dir string, file string) string {
	var s strings.Builder
	fmt.Fprintf(&s, "tftp://%s:%s/", host, port)
	if dir != "" {
		fmt.Fprintf(&s, "%s/", dir)
	}
	fmt.Fprintf(&s, "%s", file)

	return s.String()
}

func statusString(state bool) string {
	if state {
		return "on"
	}
	return "off"
}

func printHelp() string {
	var s strings.Builder
	fmt.Fprintf(&s, "not implemented yet\n")
	return s.String()
}

func readInputInteractive(in *bufio.Scanner, out io.Writer) []string {
	fmt.Fprint(out, "tftp:> ")
	in.Scan()
	return strings.Split(in.Text(), " ")
}

func readHostInteractive(in *bufio.Scanner, out io.Writer) string {
	fmt.Fprint(out, "(to): ")
	in.Scan()
	return in.Text()
}

var ErrInvalidTransferMode = errors.New("invalid transfer mode")

func validateMode(mode string) (tftp.TransferMode, error) {
	var ret tftp.TransferMode
	switch tftp.TransferMode(mode) {
	case "ascii":
		ret = tftp.ModeNetASCII
	case "binary":
		ret = tftp.ModeOctet
	default:
		return ret, ErrInvalidTransferMode
	}
	return ret, nil
}

type PutCmd struct {
	localfiles []string
	remotefile string
	remotedir  string
}

func executePut(client ClientIf, host, port string, files []string) error {
	ret := &PutCmd{}
	switch len(files) {
	case 1:
		// Put file to server
		ret.localfiles = append(ret.localfiles, files...)
	case 2:
		// files[0] == localfile
		ret.localfiles = append(ret.localfiles, files[0])
		// files[1] == remotefile
		ret.remotefile = files[1]
	default:
		// files[:len(files)-2] == localfiles,
		ret.localfiles = append(ret.localfiles, files[:len(files)-2]...)
		// files[len(files)-1] == remote-directory
		ret.remotedir = files[len(files)-1]
	}

	for _, file := range ret.localfiles {
		url := constructURL(host, port, "", file)

		if len(ret.localfiles) == 1 && ret.remotefile != "" {
			url = constructURL(host, port, "", ret.remotefile)
		} else if len(ret.localfiles) > 1 {
			url = constructURL(host, port, ret.remotedir, file)
		}

		locFile, err := os.Open(file)
		if err != nil {
			return err
		}

		fs, err := locFile.Stat()
		if err != nil {
			return err
		}
		if err := client.Put(url, locFile, fs.Size()); err != nil {
			return err
		}
	}

	return nil
}

type GetCmd struct {
	remotefiles []string
	localfile   string
}

var errSizeNoMatch = errors.New("data size of read and write mismatch")

func executeGet(client ClientIf, host, port string, files []string) error {
	ret := &GetCmd{}
	switch len(files) {
	case 1:
		// files[0] == remotefile
		ret.remotefiles = append(ret.remotefiles, files[0])
	case 2:
		// files[0] == remotefile
		ret.remotefiles = append(ret.remotefiles, files[0])
		// files[1] == localfile
		ret.localfile = files[1]
	default:
		// files... == remotefiles
		ret.remotefiles = append(ret.remotefiles, files...)
	}

	for _, file := range ret.remotefiles {
		resp, err := client.Get(constructURL(host, port, "", file))
		if err != nil {
			return err
		}

		localfile, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY, 0o666)
		if err != nil {
			return nil
		}
		defer localfile.Close()

		if ret.localfile != "" && len(ret.remotefiles) == 1 {
			localfile, err = os.OpenFile(ret.localfile, os.O_CREATE|os.O_WRONLY, 0o666)
			if err != nil {
				return err
			}
		}

		datalen, err := resp.Size()
		if err != nil {
			return err
		}

		data := make([]byte, datalen)
		nR, err := resp.Read(data)
		if err != nil {
			return err
		}

		nW, err := localfile.Write(data)
		if err != nil {
			return err
		}

		if nR != nW {
			return errSizeNoMatch
		}
	}

	return nil
}