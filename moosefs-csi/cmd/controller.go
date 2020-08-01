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
	"log"

	"github.com/spf13/cobra"

	mfscsi "github.com/Kunde21/moosefs-csi"
	mfs "github.com/Kunde21/moosefs-csi/driver"
)

// controllerCmd represents the controller command
var controllerCmd = &cobra.Command{
	Use:   "controller",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	RunE: RunController,
}

func init() {
	rootCmd.AddCommand(controllerCmd)
	controllerCmd.Flags().StringVarP(&csiArgs.mountDir, "mount", "m", "/opt/mfs/kubernetes",
		"mount point for controller directory.")
}

// RunController runs the controller server
func RunController(cmd *cobra.Command, args []string) error {
	driver := mfs.NewMFSdriver(csiArgs.nodeID, csiArgs.endpoint, csiArgs.server)
	is, err := mfs.NewIdentityServer(driver)
	if err != nil {
		return err
	}
	cs, err := mfs.NewControllerServer(driver, csiArgs.root, csiArgs.mountDir)
	if err != nil {
		return err
	}
	defer func() {
		if err := cs.Close(); err != nil {
			log.Println(err)
		}
	}()
	return mfscsi.Serve(csiArgs.endpoint, cs, is)
}
