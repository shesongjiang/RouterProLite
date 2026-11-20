package main

// Check 是预检/索引阶段的一条可读结论。
type Check struct {
	ID     string `json:"id"`
	Stage  string `json:"stage"` // index | A | B | C
	Label  string `json:"label"`
	Level  string `json:"level"` // ok | warn | fail
	Detail string `json:"detail"`
}

// PlanItem 是依赖闭包中的一个包。
type PlanItem struct {
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Arch          string   `json:"arch"`
	URL           string   `json:"url"`
	Filename      string   `json:"filename"`
	LocalPath     string   `json:"localPath"`
	Size          int64    `json:"size"`
	InstalledSize int64    `json:"installedSize"`
	Description   string   `json:"description"`
	License       string   `json:"license"`
	Feed          string   `json:"feed"`
	SHA256        string   `json:"sha256"`
	Checksum      string   `json:"checksum"`
	ChecksumKind  string   `json:"checksumKind"`
	Depends       []string `json:"depends"`
	Provides      []string `json:"provides"`
	Conflicts     []string `json:"conflicts"`
	Replaces      []string `json:"replaces"`
	Installed     bool     `json:"installed"`
	IsRoot        bool     `json:"isRoot"`
	Dangerous     bool     `json:"dangerous"`
	Reason        string   `json:"reason"`
	Status        string   `json:"status"`
	Downloaded    string   `json:"downloaded"`
	RemotePath    string   `json:"remotePath"`
}

// InstallPlan 是求解后的安装计划。
type InstallPlan struct {
	Packages     []*PlanItem `json:"packages"`
	TotalSize    int64       `json:"totalSize"`
	TotalInstall int64       `json:"totalInstall"`
	Warnings     []string    `json:"warnings"`
}

// InstallRequest 是前端发起的一次预演/安装请求。
type InstallRequest struct {
	Mode           string   `json:"mode"` // feed | local
	Package        string   `json:"package"`
	Version        string   `json:"version"`
	LocalPath      string   `json:"localPath"`
	DryRun         bool     `json:"dryRun"`
	Repos          []string `json:"repos"`          // 可用自定义仓库覆盖设备源
	Commit         bool     `json:"commit"`         // true = 真实安装
	Confirmed      bool     `json:"confirmed"`      // 用户已勾选“我确认写入设备”
	AllowDangerous bool     `json:"allowDangerous"` // 用户已确认安装危险包
	ConfirmText    string   `json:"confirmText"`    // 必须与当前设备地址一致
	ConfirmWrites  bool     `json:"confirmWrites"`  // 已确认写入 overlay 与 /tmp
}

// JobFile 是任务中每个文件的进度。
type JobFile struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"` // download | upload
	Status string `json:"status"`
	Size   int64  `json:"size"`
	Sent   int64  `json:"sent"`
}

// InstallChanges 是安装前后回读的差异。
type InstallChanges struct {
	PackagesAdded []string `json:"packagesAdded"`
	InitAdded     []string `json:"initAdded"`
	ConfigChanged []string `json:"configChanged"`
}

// InstallResult 是任务终态。
type InstallResult struct {
	Success      bool            `json:"success"`
	Code         string          `json:"code"`
	Message      string          `json:"message"`
	Rollbackable bool            `json:"rollbackable"`
	Simulate     string          `json:"simulate"`
	Installed    []string        `json:"installed"`
	Pending      []string        `json:"pending"`
	Failed       string          `json:"failed"`
	Changes      *InstallChanges `json:"changes"`
}

// InstallJobView 是暴露给前端的任务快照。
type InstallJobView struct {
	ID        string         `json:"id"`
	Phase     string         `json:"phase"`
	Percent   int            `json:"percent"`
	DryRun    bool           `json:"dryRun"`
	Running   bool           `json:"running"`
	Env       *PkgEnv        `json:"env"`
	Plan      *InstallPlan   `json:"plan"`
	Checks    []Check        `json:"checks"`
	Files     []JobFile      `json:"files"`
	Log       []string       `json:"log"`
	Result    *InstallResult `json:"result"`
	StartedAt int64          `json:"startedAt"`
	LogPath   string         `json:"logPath"`
}
