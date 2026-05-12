package hostinstall

import (
	"fmt"
	"os/exec"
	"strings"
)

// RunSSH executes a command on the remote host via ssh.
// It returns the stdout and stderr from the remote command.
func RunSSH(host string, args ...string) (stdout, stderr string, err error) {
	sshArgs := []string{host}
	sshArgs = append(sshArgs, args...)
	cmd := exec.Command("ssh", sshArgs...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return outBuf.String(), errBuf.String(), fmt.Errorf("ssh %s %s: %w", host, strings.Join(args, " "), err)
	}
	return outBuf.String(), errBuf.String(), nil
}

// SCP copies a local file to the remote host using scp.
func SCP(localPath, host, remotePath string) error {
	cmd := exec.Command("scp", localPath, host+":"+remotePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("scp %s %s:%s: %w: %s", localPath, host, remotePath, err, out)
	}
	return nil
}

// DetectRemoteArch detects the remote architecture by running
// uname -m over SSH and mapping the result to a Go arch string.
// It returns "amd64" or "arm64".
func DetectRemoteArch(host string) (string, error) {
	stdout, _, err := RunSSH(host, "uname", "-m")
	if err != nil {
		return "", fmt.Errorf("detecting remote arch: %w", err)
	}
	arch := strings.TrimSpace(stdout)
	switch arch {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported remote architecture: %s", arch)
	}
}

// RemoteFileExists checks if a file exists on the remote host.
func RemoteFileExists(host, path string) (bool, error) {
	stdout, _, err := RunSSH(host, "test", "-e", path, "&&", "echo", "yes")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(stdout) == "yes", nil
}

// SetRemotePerms sets file permissions on the remote host using chmod.
func SetRemotePerms(host, path string, mode string) error {
	_, _, err := RunSSH(host, "chmod", mode, path)
	if err != nil {
		return fmt.Errorf("setting permissions %s on %s: %w", mode, path, err)
	}
	return nil
}
