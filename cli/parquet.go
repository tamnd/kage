package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tamnd/kage/dataset"
)

// newParquetCmd groups the two columnar conversions: a ZIM archive out to a
// Parquet table, and a Parquet table back to a ZIM archive. The table is a flat
// one-row-per-entry shape ready to publish as a dataset, and the round trip is
// lossless.
func newParquetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parquet",
		Short: "Convert a ZIM archive to a Parquet dataset and back",
		Long: "parquet converts a packed ZIM archive into a columnar Parquet table, one row\n" +
			"per entry with clear columns (url, mime, title, content, extracted text), and\n" +
			"converts such a table back into a ZIM. The table is the shape a dataset host\n" +
			"like Hugging Face expects, and the conversion is lossless: a ZIM round-tripped\n" +
			"through Parquet reproduces every entry, its metadata, and the main page.",
	}
	cmd.AddCommand(newParquetExportCmd())
	cmd.AddCommand(newParquetImportCmd())
	return cmd
}

func newParquetExportCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "export <file.zim>",
		Short: "Write a Parquet table from a ZIM archive",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in := args[0]
			if out == "" {
				out = strings.TrimSuffix(in, filepath.Ext(in)) + ".parquet"
			}
			st, err := dataset.ZIMToParquet(in, out, Version)
			if err != nil {
				return err
			}
			printDatasetResult("exported", out)
			printDatasetStats(st, out)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "output path (default <name>.parquet)")
	return cmd
}

func newParquetImportCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "import <file.parquet>",
		Short: "Rebuild a ZIM archive from a Parquet table",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in := args[0]
			if out == "" {
				out = strings.TrimSuffix(in, filepath.Ext(in)) + ".zim"
			}
			st, err := dataset.ParquetToZIM(in, out, Version)
			if err != nil {
				return err
			}
			printDatasetResult("imported", out)
			printDatasetStats(st, out)
			fmt.Fprintf(os.Stderr, "  open %s\n", styleAccent.Render("kage open "+out))
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "output path (default <name>.zim)")
	return cmd
}

func printDatasetResult(verb, path string) {
	fmt.Fprintln(os.Stderr, styleOK.Render(verb)+" "+styleTitle.Render(path))
}

func printDatasetStats(st dataset.Stats, path string) {
	fmt.Fprintf(os.Stderr, "  %s %d   %s %d\n",
		styleAccent.Render("rows"), st.Rows,
		styleAccent.Render("redirects"), st.Redirects)
	fmt.Fprintf(os.Stderr, "  %s %s content\n", styleDim.Render("content"), humanBytes(st.ContentBytes))
	if fi, err := os.Stat(path); err == nil {
		fmt.Fprintf(os.Stderr, "  %s %s\n", styleAccent.Render("size"), humanBytes(fi.Size()))
	}
}
