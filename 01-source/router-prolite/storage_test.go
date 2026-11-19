package main

import (
	"os"
	"strings"
	"testing"
)

const storageFixtureUSB = `@@BLOCK
{"devices":{"sda":{"device":"/dev/sda","type":"disk","size":16000000000},"sda1":{"device":"/dev/sda1","type":"partition","size":15999000000,"mounted":false,"fstype":"vfat"}}}
@@BLKID
/dev/sda1: LABEL="UDISK" UUID="1234-ABCD" TYPE="vfat"
@@MOUNTS
/dev/root / squashfs rw 0 0
@@PARTS
   8        0   15625000 sda
   8        1   15624023 sda1
@@FS
	nodev	proc
	ext4
	vfat
@@REMOVABLE
sda 1 usb
loop0 0 -
@@END
`

const storageFixtureBlockInfo = `@@BLOCK

@@BLOCKINFO
/dev/sda: UUID="2019-09-11-15-21-05-00" LABEL="MJRO1810" TYPE="iso9660"
/dev/sda1: UUID="07E3-BFF9" LABEL="MISO_EFI" TYPE="vfat"
/dev/sda2: UUID="227513be" LABEL="extroot" MOUNT="/overlay" TYPE="ext4"
@@BLKID
@@MOUNTS
/dev/root / squashfs rw 0 0
/dev/sda2 /overlay ext4 rw 0 0
@@PARTS
   8        0   15360000 sda
   8        1       4096 sda1
   8        2   12876800 sda2
@@FS
	ext4
	vfat
@@REMOVABLE
sda 1 usb
@@END
`

func TestParseStorageMarksSystemDisk(t *testing.T) {
	got := parseStorageDevices(storageFixtureBlockInfo)
	if len(got) != 3 {
		t.Fatalf("want 3 block devices, got %d: %+v", len(got), got)
	}
	byDev := map[string]StorageDeviceView{}
	for _, d := range got {
		byDev[d.Device] = d
	}
	overlay := byDev["/dev/sda2"]
	if !overlay.Protected || !strings.Contains(overlay.Role, "系统可写层") || overlay.MountPoint != "/overlay" {
		t.Fatalf("overlay partition mark failed: %+v", overlay)
	}
	efi := byDev["/dev/sda1"]
	if !efi.Protected || !strings.Contains(efi.Role, "EFI") {
		t.Fatalf("efi partition mark failed: %+v", efi)
	}
	disk := byDev["/dev/sda"]
	if !disk.Protected || !strings.Contains(disk.Role, "整盘") {
		t.Fatalf("whole disk mark failed: %+v", disk)
	}
	if disk.Blocked == "" || overlay.Blocked == "" {
		t.Fatalf("blocked reason missing: %+v", got)
	}
}

func TestParseStorageFromBlockInfo(t *testing.T) {
	raw := `@@BLOCK
@@BLOCKINFO
/dev/sdb1: UUID="ABCD-1234" LABEL="PHOTO" TYPE="exfat"
@@BLKID
@@MOUNTS
@@PARTS
   8       17    7800000 sdb1
@@FS
	exfat
@@REMOVABLE
sdb 1 usb
@@END
`
	got := parseStorageDevices(raw)
	if len(got) != 1 {
		t.Fatalf("want 1 device, got %+v", got)
	}
	if got[0].FSType != "exfat" || got[0].Label != "PHOTO" || !got[0].FSSupported {
		t.Fatalf("block info parse failed: %+v", got[0])
	}
}

func TestParseStorageDevicesUSB(t *testing.T) {
	got := parseStorageDevices(storageFixtureUSB)
	if len(got) != 2 {
		t.Fatalf("want 2 block devices, got %d: %+v", len(got), got)
	}
	var d *StorageDeviceView
	for i := range got {
		if got[i].Device == "/dev/sda1" {
			d = &got[i]
		}
	}
	if d == nil {
		t.Fatalf("sda1 missing: %+v", got)
	}
	if d.Label != "UDISK" || d.FSType != "vfat" || d.UUID != "1234-ABCD" {
		t.Fatalf("metadata mismatch: %+v", d)
	}
	if d.SizeKB != 15624023 {
		t.Fatalf("size = %d, want 15624023", d.SizeKB)
	}
	if d.Mounted {
		t.Fatalf("should not be mounted: %+v", d)
	}
	if !d.FSSupported || !d.Removable {
		t.Fatalf("vfat/removable expected: %+v", d)
	}
	if d.MountTarget != "/mnt/sda1" {
		t.Fatalf("target = %s", d.MountTarget)
	}
	if d.Reason != "" {
		t.Fatalf("unexpected reason: %s", d.Reason)
	}
}

func TestParseStorageDevicesMountedFromProc(t *testing.T) {
	raw := strings.Replace(storageFixtureUSB, "/dev/root / squashfs rw 0 0",
		"/dev/sda1 /mnt/sda1 vfat rw,relatime 0 0", 1)
	got := parseStorageDevices(raw)
	var d *StorageDeviceView
	for i := range got {
		if got[i].Device == "/dev/sda1" {
			d = &got[i]
		}
	}
	if d == nil || !d.Mounted || d.MountPoint != "/mnt/sda1" {
		t.Fatalf("mounted parse failed: %+v", got)
	}
	if d.Reason != "" || d.Protected {
		t.Fatalf("data partition should be operable: %+v", d)
	}
}

func TestParseStorageDevicesUnsupportedFS(t *testing.T) {
	raw := strings.Replace(storageFixtureUSB, `TYPE="vfat"`, `TYPE="ntfs"`, 1)
	raw = strings.Replace(raw, `"fstype":"vfat"`, `"fstype":"ntfs"`, 1)
	got := parseStorageDevices(raw)
	var d *StorageDeviceView
	for i := range got {
		if got[i].Device == "/dev/sda1" {
			d = &got[i]
		}
	}
	if d == nil {
		t.Fatalf("sda1 missing: %+v", got)
	}
	if d.FSSupported {
		t.Fatalf("ntfs should be unsupported in this fixture")
	}
	if !strings.Contains(d.Reason, "内核未加载") {
		t.Fatalf("reason = %q", d.Reason)
	}
}

func TestParseStorageIncludesNonRemovable(t *testing.T) {
	raw := strings.Replace(storageFixtureUSB, "sda 1 usb", "sda 0 -", 1)
	got := parseStorageDevices(raw)
	if len(got) != 2 {
		t.Fatalf("internal disk must still be listed, got %+v", got)
	}
	for _, d := range got {
		if d.Removable {
			t.Fatalf("removable flag should be false: %+v", d)
		}
	}
}

func TestStoragePathValidation(t *testing.T) {
	good := []string{"/dev/sda1", "/dev/sdb12", "/dev/mmcblk0p1", "/dev/nvme0n1p2"}
	bad := []string{"/dev/sda1;reboot", "/dev/mapper/x", "/etc/passwd", "/dev/sda1 ", "/dev/sdA1"}
	for _, s := range good {
		if !storageDeviceRe.MatchString(s) {
			t.Errorf("should accept %q", s)
		}
	}
	for _, s := range bad {
		if storageDeviceRe.MatchString(s) {
			t.Errorf("should reject %q", s)
		}
	}
	if !storageMountRe.MatchString("/mnt/sda1") || storageMountRe.MatchString("/mnt/../etc") {
		t.Fatal("mount point validation failed")
	}
}

func TestParentDisk(t *testing.T) {
	cases := map[string]string{
		"sda1":      "sda",
		"sdb12":     "sdb",
		"mmcblk0p1": "mmcblk0",
		"nvme0n1p2": "nvme0n1",
		"nvme0n1":   "nvme0n1",
	}
	for in, want := range cases {
		if got := parentDisk(in); got != want {
			t.Errorf("parentDisk(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStorageErrCode(t *testing.T) {
	cases := map[string]string{
		"mount: unknown filesystem type 'ntfs'":     "FS_UNSUPPORTED",
		"mount: /mnt/sda1: device or resource busy": "STORAGE_BUSY",
		"mount: can't find /dev/sda1 in /etc/fstab": "DEVICE_NOT_FOUND",
		"umount: /mnt/sda1: not mounted":            "NOT_MOUNTED",
	}
	for in, want := range cases {
		got := storageErrCode(in, "MOUNT_FAILED")
		if got != want {
			t.Errorf("storageErrCode(%q) = %s, want %s", in, got, want)
		}
	}
	if got := storageErrCode("must be superuser", "X"); got != "PERMISSION" {
		t.Errorf("permission code = %s", got)
	}
}

func TestStorageSections(t *testing.T) {
	sec := storageSections("@@A\n1\n2\n@@B\nx\n@@END\n")
	if strings.TrimSpace(sec["A"]) != "1\n2" || strings.TrimSpace(sec["B"]) != "x" {
		t.Fatalf("sections = %#v", sec)
	}
}

// TestLiveStorageDevices 只读识别，不会挂载或写入设备。
func TestLiveStorageDevices(t *testing.T) {
	if os.Getenv("PROLITE_LIVE") != "1" {
		t.Skip("set PROLITE_LIVE=1 to run against a real device")
	}
	r, _ := liveDial(t)
	a := &App{router: r}
	view, err := a.GetStorageDevices()
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	t.Logf("devices=%d note=%q", len(view.Devices), view.Note)
	for _, d := range view.Devices {
		t.Logf("%s label=%q fs=%q size=%dKB mounted=%v mp=%q removable=%v role=%q protected=%v blocked=%q supported=%v reason=%q",
			d.Device, d.Label, d.FSType, d.SizeKB, d.Mounted, d.MountPoint, d.Removable, d.Role, d.Protected, d.Blocked, d.FSSupported, d.Reason)
	}
}
