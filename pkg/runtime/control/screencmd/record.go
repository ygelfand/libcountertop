package screencmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/display/panel"
	"github.com/ygelfand/libcountertop/pkg/display/surface"
)

type recording interface {
	Read() (surface.Encoded, error)
	Close() error
}

func Record(get func() *panel.Driver) *cobra.Command {
	return &cobra.Command{
		Use:   "record SECONDS PATH",
		Short: "Record 1–60 seconds of the composed screen to a new H.264 file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			seconds, err := strconv.Atoi(args[0])
			if err != nil || seconds < 1 || seconds > 60 {
				return fmt.Errorf("seconds must be between 1 and 60")
			}
			d := get()
			helper := d.Helper()
			if helper == nil {
				return fmt.Errorf("screen capture unavailable")
			}
			w, h := d.Native()
			stream, err := helper.Record(w, h, int(d.Orientation())/90)
			if err != nil {
				return err
			}
			defer stream.Close()
			file, err := os.OpenFile(args[1], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(seconds)*time.Second)
			defer cancel()
			frames, err := recordTo(ctx, file, stream)
			if errors.Is(err, context.DeadlineExceeded) && cmd.Context().Err() == nil &&
				frames > 0 {
				err = nil
			}
			if err := errors.Join(err, file.Close()); err != nil {
				return err
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"%s %dx%d %d frames\n",
				args[1],
				stream.Width,
				stream.Height,
				frames,
			)
			return nil
		},
	}
}

func recordTo(ctx context.Context, out io.Writer, stream recording) (int, error) {
	stop := context.AfterFunc(ctx, func() { _ = stream.Close() })
	defer stop()
	frames := 0
	for {
		frame, err := stream.Read()
		if ctx.Err() != nil {
			return frames, ctx.Err()
		}
		if err != nil {
			return frames, err
		}
		if _, err := out.Write(frame.Data); err != nil {
			return frames, err
		}
		if frame.Flags&2 == 0 {
			frames++
		}
	}
}
