/*
Copyright © 2020 Chad Kunde

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"github.com/spf13/cobra"

	mfscsi "github.com/Kunde21/moosefs-csi"
	mfs "github.com/Kunde21/moosefs-csi/driver"
)

// nodeCmd represents the node command
var nodeCmd = &cobra.Command{
	Use:        "node",
	Aliases:    []string{"n"},
	SuggestFor: []string{"no"},
	Short:      "Node driver for moosefs CSI plugin",
	Long:       `TBD`,
	RunE:       RunNode,
}

func init() { rootCmd.AddCommand(nodeCmd) }

// RunNode runs the node server
func RunNode(cmd *cobra.Command, args []string) error {
	driver := mfs.NewMFSdriver(csiArgs.nodeID, csiArgs.endpoint, csiArgs.server)
	m, err := mfs.NewMounter()
	if err != nil {
		return err
	}
	ns := mfs.NewNodeServer(driver, m, csiArgs.root)
	is, err := mfs.NewIdentityServer(driver)
	if err != nil {
		return err
	}
	return mfscsi.Serve(csiArgs.endpoint, ns, is)
}
