package hostinstall

import (
	"fmt"
)

// PrintTermiusInstructions prints the values the user should paste into
// Termius to set up RemoteForward for clipbridge.
func PrintTermiusInstructions(alias string, port int) error {
	fmt.Printf(`Open Termius → Hosts → %s → Edit → Advanced → Port Forwarding
Add a new entry:
  Type: Remote
  Local: 127.0.0.1
  Local Port: %d
  Remote: 127.0.0.1
  Remote Port: %d
Then run: clipbridge add-host %s --finish
`, alias, port, port, alias)
	return nil
}
