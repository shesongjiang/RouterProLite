package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 可移动存储：识别、挂载、卸载。只操作块设备，不安装内核模块、不改 fstab。

const storageScript = `echo '@@BLOCK'; ubus call block info 2>/dev/null
echo '@@BLOCKINFO'; /sbin/block info 2>/dev/null
echo '@@BLKID'; blkid 2>/dev/null
echo '@@MOUNTS'; cat /proc/mounts 2>/dev/null
echo '@@PARTS'; cat /proc/partitions 2>/dev/null
echo '@@FS'; cat /proc/filesystems 2>/dev/null
echo '@@REMOVABLE'; for d in /sys/block/*; do b=${d##*/}; r=$(cat "$d/removable" 2>/dev/null); u=$(readlink -f "$d" 2>/dev/null); case "$u" in *usb*) u=usb;; *) u=-;; esac; echo "$b $r $u"; done
echo '@@END'; true`

type StorageDeviceView struct {
	Device      string `json:"device"`
	Name        string `json:"name"`
	Label       string `json:"label"`
	FSType      string `json:"fsType"`
	UUID        string `json:"uuid"`
	SizeKB      int64  `json:"sizeKb"`
	Mounted     bool   `json:"mounted"`
	MountPoint  string `json:"mountPoint"`
	Removable   bool   `json:"removable"`
	Role        string `json:"role"`
	Protected   bool   `json:"protected"`
	Blocked     string `json:"blockedReason"`
	FSSupported bool   `json:"fsSupported"`
	MountTarget string `json:"mountTarget"`
	Reason      string `json:"reason"`
}

type StorageView struct {
	Connected bool                `json:"connected"`
	Host      string              `json:"host"`
	Devices   []StorageDeviceView `json:"devices"`
	Note      string              `json:"note"`
}

type StorageActionInput struct {
	Device      string `json:"device"`
	MountPoint  string `json:"mountPoint"`
	ConfirmText string `json:"confirmText"`
	Confirmed   bool   `json:"confirmed"`
}

var (
	storageDeviceRe = regexp.MustCompile(`^/dev/(sd[a-z][0-9]*|mmcblk[0-9]+p[0-9]+|mmcblk[0-9]+|nvme[0-9]+n[0-9]+p[0-9]+|nvme[0-9]+n[0-9]+)$`)
	storageMountRe  = regexp.MustCompile(`^/mnt/[A-Za-z0-9._-]{1,40}$`)
	blkidFieldRe    = regexp.MustCompile(`([A-Z]+)="([^"]*)"`)
)

// GetStorageDevices 只读识别当前可移动存储，不执行任何写入。
func (a *App) GetStorageDevices() (*StorageView, error) {
	view := &StorageView{Devices: []StorageDeviceView{}}
	host := a.router.TargetHost()
	view.Host = host
	if host == "" {
		view.Note = "未连接设备：连接后才能识别 U 盘与移动硬盘。"
		return view, nil
	}
	view.Connected = true
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	out, err := a.router.execJob(ctx, storageScript, 25*time.Second, nil)
	if err != nil {
		return nil, fmt.Errorf("读取存储设备失败：%w", err)
	}
	if out.TimedOut {
		return nil, fmt.Errorf("读取存储设备超时")
	}
	view.Devices = parseStorageDevices(out.Output)
	if len(view.Devices) == 0 {
		view.Note = "没有识别到块设备。请确认 U 盘/SD 卡已插好，或设备使用 mtd/ubi 存储。"
	}
	return view, nil
}

func (a *App) storageGuard(in StorageActionInput) error {
	host := a.router.TargetHost()
	if host == "" {
		return fmt.Errorf("未连接设备")
	}
	if strings.TrimSpace(in.ConfirmText) != host {
		return fmt.Errorf("请在确认框中逐字输入当前设备地址 %s", host)
	}
	if !in.Confirmed {
		return fmt.Errorf("请先确认挂载/卸载操作")
	}
	return nil
}

// MountStorage 在用户明确确认后挂载指定可移动分区。
func (a *App) MountStorage(in StorageActionInput) (*StorageView, error) {
	a.storageMu.Lock()
	defer a.storageMu.Unlock()
	if err := a.storageGuard(in); err != nil {
		return nil, err
	}
	if !storageDeviceRe.MatchString(strings.TrimSpace(in.Device)) {
		return nil, fmt.Errorf("DEVICE_INVALID：设备路径不合法：%s", in.Device)
	}
	view, err := a.GetStorageDevices()
	if err != nil {
		return nil, err
	}
	var dev *StorageDeviceView
	for i := range view.Devices {
		if view.Devices[i].Device == in.Device {
			dev = &view.Devices[i]
			break
		}
	}
	if dev == nil {
		return nil, fmt.Errorf("DEVICE_NOT_FOUND：未识别到 %s，设备可能已拔出", in.Device)
	}
	if dev.Protected {
		return nil, fmt.Errorf("SYSTEM_DISK：%s 是%s，%s", dev.Device, dev.Role, dev.Blocked)
	}
	if dev.Mounted {
		return nil, fmt.Errorf("MOUNT_BUSY：%s 已挂载在 %s", dev.Device, dev.MountPoint)
	}
	if dev.FSType == "" {
		return nil, fmt.Errorf("FS_UNKNOWN：未识别到文件系统，可能未格式化或分区表损坏")
	}
	if !dev.FSSupported {
		return nil, fmt.Errorf("FS_UNSUPPORTED：内核未加载 %s 文件系统支持，无法挂载（不会自动安装内核模块）", dev.FSType)
	}
	mp := strings.TrimSpace(in.MountPoint)
	if mp == "" {
		mp = dev.MountTarget
	}
	if !storageMountRe.MatchString(mp) {
		return nil, fmt.Errorf("MOUNT_POINT_INVALID：挂载点必须是 /mnt/ 下的简单名称：%s", mp)
	}

	opts := storageMountOptions(dev.FSType)
	cmd := "mkdir -p " + shq(mp) +
		"; if [ -n \"$(ls -A " + shq(mp) + " 2>/dev/null)\" ] && ! grep -qs \" " + mp + " \" /proc/mounts; then echo '@@NONEMPTY'; exit 3; fi" +
		"; mount -t " + shq(dev.FSType)
	if opts != "" {
		cmd += " -o " + shq(opts)
	}
	cmd += " " + shq(dev.Device) + " " + shq(mp) + " 2>&1"

	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), 60*time.Second)
	defer cancel()
	out, err := a.router.execJob(ctx, cmd, 60*time.Second, nil)
	if err != nil {
		return nil, fmt.Errorf("MOUNT_FAILED：%w", err)
	}
	text := strings.TrimSpace(out.Output)
	if strings.Contains(text, "@@NONEMPTY") {
		return nil, fmt.Errorf("MOUNT_BUSY：挂载点 %s 非空，请先清理或改用其它挂载点", mp)
	}
	if out.ExitCode != 0 {
		return nil, fmt.Errorf("%s：%s", storageErrCode(text, "MOUNT_FAILED"), storageErrText(text))
	}
	// 回读确认
	check, err := a.router.execJob(ctx, "grep -qs \" "+mp+" \" /proc/mounts && echo MOUNTED || echo NOTMOUNTED", 10*time.Second, nil)
	if err != nil || !strings.Contains(check.Output, "MOUNTED") {
		return nil, fmt.Errorf("MOUNT_FAILED：挂载命令已返回，但 /proc/mounts 中没有 %s", mp)
	}
	return a.GetStorageDevices()
}

// UnmountStorage 在用户明确确认后卸载指定挂载点。
func (a *App) UnmountStorage(in StorageActionInput) (*StorageView, error) {
	a.storageMu.Lock()
	defer a.storageMu.Unlock()
	if err := a.storageGuard(in); err != nil {
		return nil, err
	}
	view, err := a.GetStorageDevices()
	if err != nil {
		return nil, err
	}
	var dev *StorageDeviceView
	for i := range view.Devices {
		if view.Devices[i].Device == in.Device {
			dev = &view.Devices[i]
			break
		}
	}
	if dev == nil {
		return nil, fmt.Errorf("DEVICE_NOT_FOUND：未识别到 %s", in.Device)
	}
	if dev.Protected {
		return nil, fmt.Errorf("SYSTEM_DISK：%s 是%s，%s", dev.Device, dev.Role, dev.Blocked)
	}
	if !dev.Mounted {
		return nil, fmt.Errorf("NOT_MOUNTED：%s 当前未挂载", dev.Device)
	}
	mp := dev.MountPoint
	if !storageMountRe.MatchString(mp) {
		return nil, fmt.Errorf("MOUNT_POINT_INVALID：挂载点不在 /mnt/ 下，拒绝自动卸载：%s", mp)
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), 60*time.Second)
	defer cancel()
	out, err := a.router.execJob(ctx, "umount "+shq(mp)+" 2>&1", 60*time.Second, nil)
	if err != nil {
		return nil, fmt.Errorf("UNMOUNT_FAILED：%w", err)
	}
	text := strings.TrimSpace(out.Output)
	if out.ExitCode != 0 {
		return nil, fmt.Errorf("%s：%s", storageErrCode(text, "UNMOUNT_FAILED"), storageErrText(text))
	}
	check, _ := a.router.execJob(ctx, "grep -qs \" "+mp+" \" /proc/mounts && echo MOUNTED || echo NOTMOUNTED", 10*time.Second, nil)
	if strings.Contains(check.Output, "MOUNTED") {
		return nil, fmt.Errorf("UNMOUNT_FAILED：卸载后 %s 仍在 /proc/mounts 中", mp)
	}
	return a.GetStorageDevices()
}

func (a *App) ctxOrBackground() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func storageMountOptions(fs string) string {
	switch strings.ToLower(strings.TrimSpace(fs)) {
	case "vfat", "msdos", "exfat":
		return "iocharset=utf8,umask=000"
	case "ntfs", "ntfs3":
		return "iocharset=utf8"
	}
	return ""
}

func storageErrCode(output, fallback string) string {
	low := strings.ToLower(output)
	switch {
	case strings.Contains(low, "no such device"), strings.Contains(low, "does not exist"),
		strings.Contains(low, "can't find"), strings.Contains(low, "not found"):
		return "DEVICE_NOT_FOUND"
	case strings.Contains(low, "unknown filesystem"), strings.Contains(low, "not supported"),
		strings.Contains(low, "wrong fs type"), strings.Contains(low, "bad option"):
		return "FS_UNSUPPORTED"
	case strings.Contains(low, "busy"), strings.Contains(low, "in use"), strings.Contains(low, "resource temporarily unavailable"):
		return "STORAGE_BUSY"
	case strings.Contains(low, "permission denied"), strings.Contains(low, "must be superuser"),
		strings.Contains(low, "operation not permitted"), strings.Contains(low, "only root"):
		return "PERMISSION"
	case strings.Contains(low, "not mounted"), strings.Contains(low, "no mount point"),
		strings.Contains(low, "invalid argument"):
		return "NOT_MOUNTED"
	}
	return fallback
}

func storageErrText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "设备没有返回错误信息"
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	return strings.Join(lines, "；")
}

type blockDeviceEntry struct {
	Device     string `json:"device"`
	Type       string `json:"type"`
	Size       any    `json:"size"`
	Mounted    bool   `json:"mounted"`
	MountPoint string `json:"mountpoint"`
	FSType     string `json:"fstype"`
	UUID       string `json:"uuid"`
	Label      string `json:"label"`
}

func parseStorageDevices(raw string) []StorageDeviceView {
	sec := storageSections(raw)
	mounts := map[string]string{} // device -> mountpoint
	mountFS := map[string]string{}
	for _, line := range nonEmptyLines(sec["MOUNTS"]) {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		mounts[f[0]] = f[1]
		mountFS[f[0]] = f[2]
	}
	blkid := map[string]map[string]string{}
	for _, line := range nonEmptyLines(sec["BLKID"]) {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		dev := strings.TrimSpace(parts[0])
		m := map[string]string{}
		for _, kv := range blkidFieldRe.FindAllStringSubmatch(parts[1], -1) {
			m[kv[1]] = kv[2]
		}
		if _, ok := m["TYPE"]; !ok {
			for _, f := range strings.Fields(parts[1]) {
				if strings.HasPrefix(f, "TYPE=") {
					m["TYPE"] = strings.Trim(f[6:], "\"")
				}
			}
		}
		blkid[dev] = m
	}
	// /sbin/block info 使用同样的 key=value 形式，信息更全（含 MOUNT），优先采用。
	for _, line := range nonEmptyLines(sec["BLOCKINFO"]) {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		dev := strings.TrimSpace(parts[0])
		m := blkid[dev]
		if m == nil {
			m = map[string]string{}
			blkid[dev] = m
		}
		for _, kv := range blkidFieldRe.FindAllStringSubmatch(parts[1], -1) {
			m[kv[1]] = kv[2]
		}
		if mp := m["MOUNT"]; mp != "" {
			mounts[dev] = mp
			if m["TYPE"] != "" {
				mountFS[dev] = m["TYPE"]
			}
		}
	}
	partKB := map[string]int64{}
	for _, line := range nonEmptyLines(sec["PARTS"]) {
		f := strings.Fields(line)
		if len(f) != 4 {
			continue
		}
		if n, err := strconv.ParseInt(f[2], 10, 64); err == nil {
			partKB[f[3]] = n
		}
	}
	supportedFS := map[string]bool{}
	for _, line := range nonEmptyLines(sec["FS"]) {
		f := strings.Fields(line)
		if len(f) >= 2 {
			supportedFS[f[1]] = true
		} else if len(f) == 1 && f[0] != "nodev" {
			supportedFS[f[0]] = true
		}
	}
	removable := map[string]bool{}
	removableLines := nonEmptyLines(sec["REMOVABLE"])
	for _, line := range removableLines {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if f[1] == "1" || (len(f) > 2 && f[2] == "usb") {
			removable[f[0]] = true
		}
	}
	hasRemovableInfo := len(removableLines) > 0

	candidates := map[string]*StorageDeviceView{}
	add := func(name string, sizeBytes int64, ub *blockDeviceEntry) {
		base := strings.TrimSpace(name)
		if base == "" || strings.HasPrefix(base, "loop") || strings.HasPrefix(base, "ram") || strings.HasPrefix(base, "zram") {
			return
		}
		full := "/dev/" + base
		get := candidates[full]
		if get == nil {
			get = &StorageDeviceView{Device: full, Name: base, MountTarget: "/mnt/" + base, Label: "", FSType: ""}
			candidates[full] = get
		}
		if sizeBytes > 0 {
			get.SizeKB = sizeBytes / 1024
		} else if kb := partKB[base]; kb > 0 {
			get.SizeKB = kb
		}
		if ub != nil {
			if ub.Label != "" {
				get.Label = ub.Label
			}
			if ub.UUID != "" {
				get.UUID = ub.UUID
			}
			if ub.FSType != "" {
				get.FSType = ub.FSType
			}
			if ub.Mounted && ub.MountPoint != "" {
				get.Mounted = true
				get.MountPoint = ub.MountPoint
			}
		}
		if b := blkid[full]; b != nil {
			if b["LABEL"] != "" {
				get.Label = b["LABEL"]
			}
			if b["UUID"] != "" {
				get.UUID = b["UUID"]
			}
			if b["TYPE"] != "" {
				get.FSType = b["TYPE"]
			}
		}
		if mp, ok := mounts[full]; ok {
			get.Mounted = true
			get.MountPoint = mp
			if fs := mountFS[full]; fs != "" {
				get.FSType = fs
			}
		}
	}

	if block := strings.TrimSpace(sec["BLOCK"]); block != "" {
		var bi struct {
			Devices map[string]*blockDeviceEntry `json:"devices"`
		}
		if err := json.Unmarshal([]byte(block), &bi); err == nil && len(bi.Devices) > 0 {
			for name, d := range bi.Devices {
				if d == nil || (d.Type != "" && d.Type != "disk" && d.Type != "partition") {
					continue
				}
				if !isStorageName(name) {
					continue
				}
				add(name, anyInt64(d.Size), d)
			}
		}
	}
	if len(candidates) == 0 {
		for _, line := range nonEmptyLines(sec["PARTS"]) {
			f := strings.Fields(line)
			if len(f) != 4 || !isStorageName(f[3]) {
				continue
			}
			add(f[3], 0, nil)
		}
	}

	systemMounts := map[string]bool{"/": true, "/overlay": true, "/rom": true}
	systemDisks := map[string]bool{}
	for _, d := range candidates {
		if systemMounts[d.MountPoint] || strings.HasPrefix(d.MountPoint, "/boot") {
			systemDisks[parentDisk(d.Name)] = true
		}
	}
	out := []StorageDeviceView{}
	for _, d := range candidates {
		d.Removable = isRemovableOrLikelyUSB(d.Name, removable, hasRemovableInfo)
		sysDisk := systemDisks[parentDisk(d.Name)]
		switch {
		case d.MountPoint == "/overlay":
			d.Role = "系统可写层（overlay）"
			d.Protected = true
			d.Blocked = "系统可写层，卸载会让系统立即失去可写层，禁止操作"
		case d.MountPoint == "/":
			d.Role = "根文件系统（overlayfs）"
			d.Protected = true
			d.Blocked = "根文件系统，禁止卸载"
		case d.MountPoint == "/rom":
			d.Role = "系统只读镜像（squashfs）"
			d.Protected = true
			d.Blocked = "系统只读镜像，禁止操作"
		case strings.HasPrefix(d.MountPoint, "/boot"):
			d.Role = "启动分区"
			d.Protected = true
			d.Blocked = "启动分区，禁止操作"
		case sysDisk && strings.Contains(strings.ToUpper(d.Label), "EFI"):
			d.Role = "EFI 启动分区"
			d.Protected = true
			d.Blocked = "系统启动盘的 EFI 分区，禁止操作"
		case sysDisk && parentDisk(d.Name) == d.Name:
			d.Role = "系统运行盘（整盘）"
			d.Protected = true
			d.Blocked = "整盘包含当前系统的挂载分区，禁止挂载/卸载"
		case sysDisk:
			d.Role = "系统运行盘分区"
		case d.Mounted:
			d.Role = "已挂载数据盘"
		case d.Removable:
			d.Role = "可移动数据分区"
		default:
			d.Role = "内置数据分区"
		}
		if d.FSType == "" {
			d.Reason = "未识别到文件系统，可能未格式化或分区表损坏"
		} else if !supportedFS[strings.ToLower(d.FSType)] && !strings.HasPrefix(strings.ToLower(d.FSType), "fuse") {
			d.FSSupported = false
			d.Reason = "内核未加载 " + d.FSType + " 文件系统支持"
		} else {
			d.FSSupported = true
		}
		if d.Protected {
			d.Reason = ""
		}
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Device < out[j].Device })
	return out
}

func isStorageName(name string) bool {
	switch {
	case strings.HasPrefix(name, "sd"), strings.HasPrefix(name, "mmcblk"), strings.HasPrefix(name, "nvme"):
		return true
	}
	return false
}

func isRemovableOrLikelyUSB(name string, removable map[string]bool, hasInfo bool) bool {
	if removable[name] {
		return true
	}
	parent := parentDisk(name)
	if parent != name && removable[parent] {
		return true
	}
	if hasInfo {
		return false
	}
	// 没有 /sys 信息时，允许 sd* 作为兜底（绝大多数 U 盘/移动硬盘）。
	return strings.HasPrefix(name, "sd")
}

var storagePartSuffixRe = regexp.MustCompile(`^(mmcblk[0-9]+|nvme[0-9]+n[0-9]+)p[0-9]+$`)

func parentDisk(name string) string {
	if m := storagePartSuffixRe.FindStringSubmatch(name); m != nil {
		return m[1]
	}
	if strings.HasPrefix(name, "sd") {
		return strings.TrimRight(name, "0123456789")
	}
	return name
}

func anyInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i
	}
	return 0
}

func nonEmptyLines(s string) []string {
	out := []string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// storageSections 解析形如 @@NAME 的段落输出。
func storageSections(raw string) map[string]string {
	out := map[string]string{}
	cur := ""
	var b strings.Builder
	flush := func() {
		if cur != "" {
			out[cur] = b.String()
		}
		b.Reset()
	}
	for _, line := range strings.Split(raw, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "@@") {
			flush()
			cur = strings.TrimPrefix(trim, "@@")
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	flush()
	return out
}
