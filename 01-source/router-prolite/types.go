package main

type ConnectionInput struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Auth       string `json:"auth"`
	KeyPath    string `json:"keyPath"`
	Passphrase string `json:"passphrase"`
	Password   string `json:"password"`
	Remember   bool   `json:"remember"`
}

type ConnectionInfo struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	User         string `json:"user"`
	Fingerprint  string `json:"fingerprint"`
	Hostname     string `json:"hostname"`
	Model        string `json:"model"`
	BoardName    string `json:"boardName"`
	Chipset      string `json:"chipset"`
	Kernel       string `json:"kernel"`
	Distribution string `json:"distribution"`
	Release      string `json:"release"`
	ConnectedAt  int64  `json:"connectedAt"`
}

type ConfigView struct {
	Saved       bool   `json:"saved"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	Auth        string `json:"auth"`
	KeyPath     string `json:"keyPath"`
	Fingerprint string `json:"fingerprint"`
	Pinned      bool   `json:"pinned"`
	Platform    string `json:"platform"`
	Version     string `json:"version"`
}

type Capabilities struct {
	Wireless       bool `json:"wireless"`
	Cellular       bool `json:"cellular"`
	SQM            bool `json:"sqm"`
	AdGuard        bool `json:"adguard"`
	DoH            bool `json:"doh"`
	DDNS           bool `json:"ddns"`
	FullCone       bool `json:"fullcone"`
	TrafficControl bool `json:"tc"`
	Mwan3          bool `json:"mwan3"`
	Temperature    bool `json:"temperature"`
	Proxy          bool `json:"proxy"`
	DNSMasq        bool `json:"dnsmasq"`
}

type SystemInfo struct {
	Hostname      string  `json:"hostname"`
	Model         string  `json:"model"`
	Chipset       string  `json:"chipset"`
	Kernel        string  `json:"kernel"`
	Distribution  string  `json:"distribution"`
	Release       string  `json:"release"`
	Uptime        int64   `json:"uptime"`
	CPUPct        float64 `json:"cpuPct"`
	CPUCores      int     `json:"cpuCores"`
	MemPct        float64 `json:"memPct"`
	MemUsed       int64   `json:"memUsed"`
	MemTotal      int64   `json:"memTotal"`
	Load1         float64 `json:"load1"`
	Load5         float64 `json:"load5"`
	Load15        float64 `json:"load15"`
	ProcessCount  int     `json:"processCount"`
	TempC         float64 `json:"tempC"`
	TempAvailable bool    `json:"tempAvailable"`
	FlashFree     int64   `json:"flashFree"`
	FlashTotal    int64   `json:"flashTotal"`
}

type WANInfo struct {
	Up        bool     `json:"up"`
	Status    string   `json:"status"`
	Proto     string   `json:"proto"`
	Device    string   `json:"device"`
	IP        string   `json:"ip"`
	Gateway   string   `json:"gateway"`
	DNS       []string `json:"dns"`
	Uptime    int64    `json:"uptime"`
	LinkSpeed string   `json:"linkSpeed"`
}

type Rates struct {
	WanRx     float64 `json:"wanRx"`
	WanTx     float64 `json:"wanTx"`
	LanRx     float64 `json:"lanRx"`
	LanTx     float64 `json:"lanTx"`
	LatencyMs float64 `json:"latencyMs"`
	LatencyOK bool    `json:"latencyOk"`
}

type PortStatus struct {
	Key     string  `json:"key"`
	Label   string  `json:"label"`
	Up      bool    `json:"up"`
	State   string  `json:"state"`
	Detail  string  `json:"detail"`
	Speed   string  `json:"speed,omitempty"`
	Rx      float64 `json:"rx"`
	Tx      float64 `json:"tx"`
	Clients int     `json:"clients,omitempty"`
}

type Client struct {
	Name           string  `json:"name"`
	IP             string  `json:"ip"`
	MAC            string  `json:"mac"`
	Type           string  `json:"type"`
	Online         bool    `json:"online"`
	LeaseRemaining int64   `json:"leaseRemaining"`
	ExpiresAt      int64   `json:"expiresAt"`
	Static         bool    `json:"static"`
	Blocked        bool    `json:"blocked"`
	RxBps          float64 `json:"rxBps"`
	TxBps          float64 `json:"txBps"`
	Signal         int     `json:"signal"`
}

type Tile struct {
	Key        string `json:"key"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Subtitle   string `json:"subtitle"`
	Detail     string `json:"detail,omitempty"`
	Tone       string `json:"tone"`
	Active     bool   `json:"active"`
	Toggleable bool   `json:"toggleable"`
	On         bool   `json:"on"`
	Action     string `json:"action"`
}

type Snapshot struct {
	Connected   bool            `json:"connected"`
	Error       string          `json:"error,omitempty"`
	RefreshedAt int64           `json:"refreshedAt"`
	Info        *ConnectionInfo `json:"info,omitempty"`
	System      *SystemInfo     `json:"system,omitempty"`
	WAN         *WANInfo        `json:"wan,omitempty"`
	Rates       *Rates          `json:"rates,omitempty"`
	Clients     []Client        `json:"clients"`
	Tiles       []Tile          `json:"tiles"`
	Ports       []PortStatus    `json:"ports"`
	Plugins     []PluginSummary `json:"plugins"`
	Caps        Capabilities    `json:"caps"`
}

type PluginSummary struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Enabled bool   `json:"enabled"`
}

type PortForward struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Proto    string `json:"proto"`
	ExtPort  string `json:"extPort"`
	DestIP   string `json:"destIp"`
	DestPort string `json:"destPort"`
	Enabled  bool   `json:"enabled"`
}

type PortForwardInput struct {
	Name     string `json:"name"`
	Proto    string `json:"proto"`
	ExtPort  string `json:"extPort"`
	DestIP   string `json:"destIp"`
	DestPort string `json:"destPort"`
}

type StaticLeaseInput struct {
	MAC     string `json:"mac"`
	IP      string `json:"ip"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type DNSSettings struct {
	Servers   []string `json:"servers"`
	Effective []string `json:"effective"`
	DoH       bool     `json:"doh"`
}

type DDNSService struct {
	Name    string `json:"name"`
	Domain  string `json:"domain"`
	Enabled bool   `json:"enabled"`
	Running bool   `json:"running"`
}

type DDNSInfo struct {
	Installed bool          `json:"installed"`
	Services  []DDNSService `json:"services"`
}

type ServiceStatus struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Enabled bool   `json:"enabled"`
}

type BlockRule struct {
	MAC     string `json:"mac"`
	Section string `json:"section"`
}

type CellularInfo struct {
	Present   bool     `json:"present"`
	Installed bool     `json:"installed"`
	Internet  bool     `json:"internet"`
	Connected bool     `json:"connected"`
	Count     int      `json:"count"`
	Mode      string   `json:"mode,omitempty"`
	Operator  string   `json:"operator,omitempty"`
	Model     string   `json:"model,omitempty"`
	SIMStatus string   `json:"simStatus,omitempty"`
	Band      string   `json:"band,omitempty"`
	Interface string   `json:"interface,omitempty"`
	RSRP      int      `json:"rsrp,omitempty"`
	RSRQ      int      `json:"rsrq,omitempty"`
	SINR      int      `json:"sinr,omitempty"`
	RSSI      int      `json:"rssi,omitempty"`
	IPv4      string   `json:"ipv4,omitempty"`
	Gateway   string   `json:"gateway,omitempty"`
	DNS       []string `json:"dns,omitempty"`
	Uptime    int64    `json:"uptime,omitempty"`
	Raw       string   `json:"raw"`
	Devices   []string `json:"devices"`
}

type MWANInterface struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type MWANInfo struct {
	Installed  bool            `json:"installed"`
	Running    bool            `json:"running"`
	Interfaces []MWANInterface `json:"interfaces"`
}

type WirelessInfo struct {
	Present bool     `json:"present"`
	Enabled bool     `json:"enabled"`
	SSID    string   `json:"ssid"`
	Channel string   `json:"channel"`
	Radios  []string `json:"radios"`
}

type LabData struct {
	Info         *ConnectionInfo `json:"info"`
	Caps         Capabilities    `json:"caps"`
	WAN          WANInfo         `json:"wan"`
	PortForwards []PortForward   `json:"portForwards"`
	DNS          DNSSettings     `json:"dns"`
	DDNS         DDNSInfo        `json:"ddns"`
	FullCone     bool            `json:"fullcone"`
	Services     []ServiceStatus `json:"services"`
	Cellular     CellularInfo    `json:"cellular"`
	Mwan         MWANInfo        `json:"mwan"`
	Blocked      []BlockRule     `json:"blocked"`
	Wireless     WirelessInfo    `json:"wireless"`
	Fingerprint  string          `json:"fingerprint"`
}

type ProbeResult struct {
	Command    string  `json:"command"`
	Output     string  `json:"output"`
	ExitCode   int     `json:"exitCode"`
	DurationMs float64 `json:"durationMs"`
}

type MemoryInfo struct {
	Total     int64 `json:"total"`
	Free      int64 `json:"free"`
	Available int64 `json:"available"`
	Buffers   int64 `json:"buffers"`
	Cached    int64 `json:"cached"`
	SwapTotal int64 `json:"swapTotal"`
	SwapFree  int64 `json:"swapFree"`
}

type StorageInfo struct {
	Filesystem string `json:"filesystem"`
	Size       string `json:"size"`
	Used       string `json:"used"`
	Available  string `json:"available"`
	UsePct     string `json:"usePct"`
	Mount      string `json:"mount"`
}

type MountInfo struct {
	Device  string `json:"device"`
	Mount   string `json:"mount"`
	FS      string `json:"fs"`
	Options string `json:"options"`
}

type InterfaceInfo struct {
	Name      string   `json:"name"`
	Up        bool     `json:"up"`
	Pending   bool     `json:"pending"`
	Available bool     `json:"available"`
	Proto     string   `json:"proto"`
	Device    string   `json:"device"`
	IPs       []string `json:"ips"`
	Gateway   string   `json:"gateway"`
	DNS       []string `json:"dns"`
	Uptime    int64    `json:"uptime"`
	RxBytes   uint64   `json:"rxBytes"`
	TxBytes   uint64   `json:"txBytes"`
	LinkSpeed string   `json:"linkSpeed"`
	CellModem bool     `json:"cellModem,omitempty"`
}

type WiFiRadio struct {
	Section  string `json:"section"`
	Name     string `json:"name"`
	Band     string `json:"band"`
	Channel  string `json:"channel"`
	Width    string `json:"width"`
	TxPower  string `json:"txPower"`
	Country  string `json:"country"`
	Path     string `json:"path"`
	Disabled bool   `json:"disabled"`
	Active   bool   `json:"active"`
}

type WiFiIface struct {
	Section    string `json:"section"`
	Radio      string `json:"radio"`
	Mode       string `json:"mode"`
	SSID       string `json:"ssid"`
	Network    string `json:"network"`
	Encryption string `json:"encryption"`
	Key        string `json:"key"`
	Channel    string `json:"channel"`
	Hidden     bool   `json:"hidden"`
	Disabled   bool   `json:"disabled"`
	Active     bool   `json:"active"`
	Clients    int    `json:"clients"`
	Signal     string `json:"signal"`
	Bitrate    string `json:"bitrate"`
}

type WirelessDetail struct {
	Present bool        `json:"present"`
	Radios  []WiFiRadio `json:"radios"`
	Ifaces  []WiFiIface `json:"ifaces"`
}

type PluginInfo struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Package   string `json:"package"`
	Config    string `json:"config"`
	Init      string `json:"init"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Running   bool   `json:"running"`
}

type PackageInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ProcessInfo struct {
	PID     int    `json:"pid"`
	User    string `json:"user"`
	VSZ     int64  `json:"vsz"`
	Stat    string `json:"stat"`
	Command string `json:"command"`
}

type DHCPPool struct {
	Section   string `json:"section"`
	Start     string `json:"start"`
	Limit     string `json:"limit"`
	LeaseTime string `json:"leaseTime"`
	Interface string `json:"interface"`
	Ignore    bool   `json:"ignore"`
}

type StaticHost struct {
	Section string `json:"section"`
	Name    string `json:"name"`
	IP      string `json:"ip"`
	MAC     string `json:"mac"`
}

type LeaseView struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	Expires  int64  `json:"expires"`
}

type DHCPDetail struct {
	Pools       []DHCPPool   `json:"pools"`
	StaticHosts []StaticHost `json:"staticHosts"`
	Leases      []LeaseView  `json:"leases"`
}

type FirewallZone struct {
	Name     string   `json:"name"`
	Networks []string `json:"networks"`
	Input    string   `json:"input"`
	Output   string   `json:"output"`
	Forward  string   `json:"forward"`
	Masq     bool     `json:"masq"`
}

type FirewallDetail struct {
	Zones          []FirewallZone `json:"zones"`
	RuleCount      int            `json:"ruleCount"`
	RedirectCount  int            `json:"redirectCount"`
	FullCone       bool           `json:"fullcone"`
	UPnPInstalled  bool           `json:"upnpInstalled"`
	UPnPRunning    bool           `json:"upnpRunning"`
	UPnPEnabled    bool           `json:"upnpEnabled"`
	UPnPSection    string         `json:"upnpSection"`
	UPnPConfigName string         `json:"upnpConfigName"`
}

type ConsoleData struct {
	Board        BoardInfo         `json:"board"`
	Release      map[string]string `json:"release"`
	Uname        string            `json:"uname"`
	Hostname     string            `json:"hostname"`
	CPUModel     string            `json:"cpuModel"`
	CPUCores     int               `json:"cpuCores"`
	CPUFreqMHz   int               `json:"cpuFreqMhz"`
	Arch         string            `json:"arch"`
	Kernel       string            `json:"kernel"`
	Uptime       int64             `json:"uptime"`
	Date         string            `json:"date"`
	Timezone     string            `json:"timezone"`
	ZoneName     string            `json:"zonename"`
	SystemSec    string            `json:"systemSection"`
	NTPSection   string            `json:"ntpSection"`
	NTPEnabled   bool              `json:"ntpEnabled"`
	NTPServers   []string          `json:"ntpServers"`
	Memory       MemoryInfo        `json:"memory"`
	Storage      []StorageInfo     `json:"storage"`
	Mounts       []MountInfo       `json:"mounts"`
	Interfaces   []InterfaceInfo   `json:"interfaces"`
	Wireless     WirelessDetail    `json:"wireless"`
	Plugins      []PluginInfo      `json:"plugins"`
	Packages     []PackageInfo     `json:"packages"`
	PackageCount int               `json:"packageCount"`
	Processes    []ProcessInfo     `json:"processes"`
	DHCP         DHCPDetail        `json:"dhcp"`
	Firewall     FirewallDetail    `json:"firewall"`
	Modem        CellularInfo      `json:"modem"`
	Mwan         MWANInfo          `json:"mwan"`
	Configs      []string          `json:"configs"`
	FetchedAt    int64             `json:"fetchedAt"`
}

type UCIOptionInput struct {
	Config  string `json:"config"`
	Section string `json:"section"`
	Option  string `json:"option"`
	Value   string `json:"value"`
}

type UCIListInput struct {
	Config  string   `json:"config"`
	Section string   `json:"section"`
	Option  string   `json:"option"`
	Values  []string `json:"values"`
}

type SystemInput struct {
	Section    string   `json:"section"`
	Hostname   string   `json:"hostname"`
	Timezone   string   `json:"timezone"`
	ZoneName   string   `json:"zonename"`
	NTPSection string   `json:"ntpSection"`
	NTPServers []string `json:"ntpServers"`
}

type WiFiInput struct {
	Section      string `json:"section"`
	SSID         string `json:"ssid"`
	Key          string `json:"key"`
	Encryption   string `json:"encryption"`
	Disabled     bool   `json:"disabled"`
	RadioSection string `json:"radioSection"`
	Channel      string `json:"channel"`
	Width        string `json:"width"`
	TxPower      string `json:"txPower"`
}

type DHCPPoolInput struct {
	Section   string `json:"section"`
	Start     string `json:"start"`
	Limit     string `json:"limit"`
	LeaseTime string `json:"leaseTime"`
}
