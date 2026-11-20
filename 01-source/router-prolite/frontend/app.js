'use strict';
(function () {
  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

  const MOCK = new URLSearchParams(location.search).has('mock');
  const NO_AUTOCONNECT = new URLSearchParams(location.search).get('connect') === '0';

  const state = {
    api: null,
    cfg: null,
    info: null,
    snap: null,
    lab: null,
    connected: false,
    linkDown: false,
    lastConn: null,
    leaving: false,
    auth: 'none',
    theme: 'dark',
    ipMasked: false,
    pinned: false,
    expandedMac: '',
    tileSig: '',
    clientSig: '',
    labOpen: false,
    labTab: 'console',
    labLoading: false,
    console: null,
    consoleLoading: false,
    consoleError: '',
    cTab: 'overview',
    cfgName: '',
    cfgList: [],
    cfgVals: null,
    cfgLoading: false,
    pkgFilter: '',
    logKind: 'system',
    logFilter: '',
    logOut: '',
    logLoading: false,
    accOpen: new Set(['pf', 'probe']),
    pfFormOpen: false,
    pfDraft: { name: '', proto: 'tcp', ext: '', destPort: '', destIP: '' },
    dnsDraft: null,
    opMsgs: {},
    probeTarget: '1.1.1.1',
    probeBusy: false,
    probeOut: null,
    cmdBusy: false,
    cmdOut: null,
    cmdLast: '',
    cmdHistory: [],
    cmdIdx: -1,
    pollTimer: null,
    pollBusy: false,
    pollGen: 0,
    lastPollError: 0,
    toastTimes: {},
    storage: {
      view: null,
      loading: false,
      err: '',
      busy: false,
    },
    market: {
      env: null,
      envErr: '',
      reposText: '',
      reposTouched: false,
      index: null,
      indexErr: '',
      loading: false,
      checks: [],
      repoOpen: false,
      search: '',
      cat: '',
      onlyNew: false,
      sort: 'name',
      page: 0,
      pageSize: 10,
      selected: null,
      plan: null,
      planLoading: false,
      planErr: '',
      confirmed: false,
      allowDangerous: false,
      policy: null,
      policyErr: '',
      confirmHost: '',
      confirmA: false,
      confirmB: false,
      starting: false,
      jobId: '',
      job: null,
      pollTimer: null,
    },
  };

  /* ================= utils ================= */
  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"']/g, (c) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));

  function errText(e) {
    let s = '';
    if (typeof e === 'string') s = e;
    else if (e && e.message) s = e.message;
    else if (e != null) s = String(e);
    s = s.replace(/^Error:\s*/i, '').trim();
    return s || '操作失败';
  }

  function rateParts(bps) {
    bps = Math.max(0, Number(bps) || 0);
    if (bps >= 1073741824) {
      const v = bps / 1073741824;
      return { v: v.toFixed(v >= 100 ? 0 : 1), u: 'GB/s' };
    }
    if (bps >= 1048576) {
      const v = bps / 1048576;
      return { v: v.toFixed(v >= 100 ? 0 : 1), u: 'MB/s' };
    }
    if (bps >= 1024) return { v: (bps / 1024).toFixed(0), u: 'KB/s' };
    return { v: String(Math.round(bps)), u: 'B/s' };
  }
  const rateText = (bps) => { const p = rateParts(bps); return p.v + ' ' + p.u; };
  const rateHTML = (bps) => { const p = rateParts(bps); return p.v + '<span class="unit">' + p.u + '</span>'; };
  function rateShort(bps) {
    bps = Math.max(0, Number(bps) || 0);
    if (bps >= 1073741824) return (bps / 1073741824).toFixed(1) + 'G';
    if (bps >= 1048576) return (bps / 1048576).toFixed(1) + 'M';
    if (bps >= 1024) return (bps / 1024).toFixed(0) + 'K';
    return Math.round(bps) + 'B';
  }

  function clipText(s, n) {
    s = String(s == null ? '' : s);
    return s.length > n ? s.slice(0, n - 1) + '…' : s;
  }

  function fmtBytes(n) {
    n = Number(n) || 0;
    if (!n) return '—';
    if (n >= 1073741824) return (n / 1073741824).toFixed(1) + ' GB';
    if (n >= 1048576) return (n / 1048576).toFixed(n < 10485760 ? 1 : 0) + ' MB';
    if (n >= 1024) return (n / 1024).toFixed(0) + ' KB';
    return n + ' B';
  }

  function shortDur(sec) {
    sec = Math.max(0, Math.round(Number(sec) || 0));
    const d = Math.floor(sec / 86400);
    const h = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60);
    if (d > 0) return d + 'd ' + h + 'h';
    if (h > 0) return h + 'h ' + m + 'm';
    if (m > 0) return m + 'm';
    return sec + 's';
  }

  function fmtDur(ms) {
    ms = Number(ms) || 0;
    if (ms >= 1000) return (ms / 1000).toFixed(1) + 's';
    return Math.round(ms) + 'ms';
  }

  function leaseText(c) {
    if (c.static) return '静态租约';
    if (c.leaseRemaining < 0) return '手工 / 静态';
    if (c.leaseRemaining > 0) return '租约 ' + shortDur(c.leaseRemaining);
    return '租约已过期';
  }

  function shortFp(fp) {
    if (!fp) return '未记录';
    const body = fp.replace(/^SHA256:/, '');
    return 'SHA256:' + body.slice(0, 16) + '…';
  }

  const cssNum = (v) => Math.max(0, Math.min(100, Number(v) || 0));

  /* ================= toast ================= */
  function toast(msg, kind = 'info', ms) {
    const host = $('#toasts');
    if (!host) return;
    const el = document.createElement('div');
    el.className = 'toast ' + kind;
    const icon = kind === 'ok' ? 'check' : kind === 'error' ? 'alert' : 'bolt';
    el.innerHTML = '<svg class="i"><use href="#i-' + icon + '"/></svg><span>' + esc(msg) + '</span>';
    host.appendChild(el);
    const ttl = ms || (kind === 'error' ? 5200 : 3200);
    setTimeout(() => {
      el.classList.add('leaving');
      setTimeout(() => el.remove(), 280);
    }, ttl);
  }

  function toastError(e) {
    const msg = errText(e);
    if (msg.indexOf('需要重新输入密码') >= 0 || msg.indexOf('凭据已失效') >= 0) {
      leaveToConnect(msg);
      return;
    }
    const now = Date.now();
    if (state.toastTimes[msg] && now - state.toastTimes[msg] < 4000) return;
    state.toastTimes[msg] = now;
    toast(msg, 'error');
  }

  async function copyText(text, label) {
    if (!text || text === '—') return;
    try {
      if (state.api) await state.api.CopyText(text);
      else await navigator.clipboard.writeText(text);
      toast(label + ' 已复制', 'ok', 1800);
    } catch (e) {
      toastError('复制失败：' + errText(e));
    }
  }

  /* ================= theme ================= */
  function applyTheme(theme) {
    state.theme = theme === 'light' ? 'light' : 'dark';
    document.documentElement.dataset.theme = state.theme;
    try { localStorage.setItem('prolite-theme', state.theme); } catch (e) {}
    const icon = state.theme === 'dark' ? 'sun' : 'moon';
    $$('#themeBtn use, #connectThemeBtn use').forEach((u) => u.setAttribute('href', '#i-' + icon));
  }

  function toggleTheme() {
    applyTheme(state.theme === 'dark' ? 'light' : 'dark');
  }

  /* ================= api ================= */
  function makeWailsApi() {
    const w = window.go && window.go.main && window.go.main.App;
    if (!w) return null;
    const call = (name) => (...args) => {
      if (typeof w[name] !== 'function') return Promise.reject(new Error('运行时缺少方法 ' + name));
      return w[name](...args);
    };
    const names = [
      'GetConfig', 'Connect', 'Disconnect', 'GetSnapshot', 'GetLab', 'SetAlwaysOnTop',
      'CloseApp', 'WindowMinimise', 'WindowToggleMaximise', 'WindowIsMaximised', 'PickKeyFile', 'ResetFingerprint', 'CopyText', 'AddPortForward',
      'TogglePortForward', 'DeletePortForward', 'SetStaticLease', 'SetBlocked', 'SetDNS',
      'SetFullCone', 'ToggleService', 'WanAction', 'ReloadNetwork', 'RebootRouter',
      'RunProbe', 'ExecCommand', 'GetConsole', 'GetLogs', 'GetConfigs', 'GetConfigValues',
      'SetUCIOption', 'SetUCIList', 'DeleteUCIOption', 'SetSystem', 'SetWiFi',
      'SetDHCPPool', 'SetUPnP', 'InterfaceAction', 'ReloadService',
      'GetPkgEnv', 'InspectLocalPackage', 'PickPackageFile', 'StartInstallJob',
      'CancelInstallJob', 'GetInstallJob', 'GetMarketRepos', 'GetMarketIndex', 'ResolvePlan',
      'GetDeviceWritePolicy',
    ];
    const api = {};
    names.forEach((n) => { api[n] = call(n); });
    return api;
  }

  let mockStorageDevices = null;
  function mockStorageView() {
    if (!mockStorageDevices) {
      mockStorageDevices = [
        { device: '/dev/sda', name: 'sda', label: 'MJRO1810', fsType: 'iso9660', uuid: '2019-09-11-15-21-05-00', sizeKb: 15360000, mounted: false, mountPoint: '', removable: true, role: '系统运行盘（整盘）', protected: true, blockedReason: '整盘包含当前系统的挂载分区，禁止挂载/卸载', fsSupported: true, mountTarget: '/mnt/sda', reason: '' },
        { device: '/dev/sda1', name: 'sda1', label: 'MISO_EFI', fsType: 'vfat', uuid: '07E3-BFF9', sizeKb: 4096, mounted: false, mountPoint: '', removable: true, role: 'EFI 启动分区', protected: true, blockedReason: '系统启动盘的 EFI 分区，禁止操作', fsSupported: true, mountTarget: '/mnt/sda1', reason: '' },
        { device: '/dev/sda2', name: 'sda2', label: 'extroot', fsType: 'ext4', uuid: '227513be-7afa-472d-ae3d-5ace88494355', sizeKb: 12876800, mounted: true, mountPoint: '/overlay', removable: true, role: '系统可写层（overlay）', protected: true, blockedReason: '系统可写层，卸载会让系统立即失去可写层，禁止操作', fsSupported: true, mountTarget: '/mnt/sda2', reason: '' },
        { device: '/dev/sdb1', name: 'sdb1', label: 'BACKUP', fsType: 'ext4', uuid: '5f2c9a1e', sizeKb: 976762584, mounted: true, mountPoint: '/mnt/sdb1', removable: true, role: '已挂载数据盘', protected: false, blockedReason: '', fsSupported: true, mountTarget: '/mnt/sdb1', reason: '' },
        { device: '/dev/sdc1', name: 'sdc1', label: '', fsType: '', uuid: '', sizeKb: 31250000, mounted: false, mountPoint: '', removable: true, role: '可移动数据分区', protected: false, blockedReason: '', fsSupported: false, mountTarget: '/mnt/sdc1', reason: '未识别到文件系统，可能未格式化或分区表损坏' },
      ];
    }
    return { connected: true, host: '192.168.100.1', devices: mockStorageDevices.map((d) => ({ ...d })), note: '' };
  }

  function mockApi() {
    const t0 = Date.now();
    const startedAt = Math.floor(Date.now() / 1000);
    let connected = false;
    let cfg = {
      saved: true, host: '192.168.100.1', port: 22, user: 'root', auth: 'none',
      keyPath: '', fingerprint: 'SHA256:jX9uQ2m4bY7wC1kR5tP8zN3vH6sL0aD2eF4gJ7kM9pQ', pinned: false,
      platform: 'win32', version: '0.1.0',
    };
    let wifiOn = true, sqmOn = true, adOn = true, proxyOn = true, fullcone = true, wanUp = true;
    let dns = ['223.5.5.5', '119.29.29.29'];
    let pfs = [
      { id: 'pf1', name: 'NAS Web', proto: 'tcp', extPort: '8443', destIp: '192.168.1.10', destPort: '443', enabled: true },
      { id: 'pf2', name: 'Workstation RDP', proto: 'tcp', extPort: '3389', destIp: '192.168.1.101', destPort: '3389', enabled: false },
      { id: 'pf3', name: 'Game UDP', proto: 'udp', extPort: '3074', destIp: '192.168.1.101', destPort: '3074', enabled: true },
    ];
    const baseClients = [
      { name: 'Workstation-PC', ip: '192.168.1.101', mac: '00:1a:2b:3c:4d:5e', type: '2.5G LAN', online: true, static: true, rxb: 54.2, txb: 8.1 },
      { name: 'MacBook-Pro', ip: '192.168.1.105', mac: '3c:22:fb:11:22:33', type: '5G', online: true, static: false, rxb: 14.6, txb: 2.2 },
      { name: 'TrueNAS-Core', ip: '192.168.1.10', mac: '00:11:32:aa:bb:cc', type: '2.5G LAN', online: true, static: true, rxb: 3.2, txb: 0.4 },
      { name: 'iPhone-15', ip: '192.168.1.112', mac: '9c:fc:01:44:55:66', type: '5G', online: false, static: false, rxb: 0, txb: 0 },
    ];
    let clients = baseClients.map((c) => ({ ...c, blocked: false }));
    let blocked = [];
    let services = [
      { key: 'dnsmasq', name: 'DNS / DHCP', running: true, enabled: true },
      { key: 'firewall', name: '防火墙', running: true, enabled: true },
      { key: 'dropbear', name: 'SSH 服务', running: true, enabled: true },
      { key: 'sqm', name: 'SQM QoS', running: sqmOn, enabled: sqmOn },
      { key: 'AdGuardHome', name: 'AdGuard Home', running: adOn, enabled: adOn },
      { key: 'https-dns-proxy', name: 'DoH 代理', running: false, enabled: false },
      { key: 'mwan3', name: 'MWAN3 多线', running: false, enabled: false },
    ];
    const caps = {
      wireless: false, cellular: true, sqm: true, adguard: true, doh: false, ddns: true,
      fullcone: true, tc: true, mwan3: false, temperature: true, proxy: true, dnsmasq: true,
    };
    const info = {
      host: '192.168.100.1', port: 22, user: 'root',
      fingerprint: cfg.fingerprint,
      hostname: 'OpenWrt-X86', model: 'ProLite Virtual Router', boardName: 'x86_64',
      chipset: 'N100', kernel: '6.6.73', distribution: 'ImmortalWrt',
      release: 'ImmortalWrt 23.05-SNAPSHOT', connectedAt: startedAt,
    };
    const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

    function tiles() {
      const out = [];
      out.push({ key: 'cellular', title: '蜂窝网络', status: '4G', subtitle: 'CHINA MOBILE', tone: 'green', active: true, toggleable: false, on: true, action: 'cell:detail', detail: '已连接 · usb0 · RSRP -78 dBm' });
      if (sqmOn !== null) out.push({ key: 'sqm', title: 'SQM QoS', status: sqmOn ? 'CAKE' : 'OFF', subtitle: '抗缓冲膨胀 · 低抖动', tone: 'green', active: sqmOn, toggleable: true, on: sqmOn, action: 'toggle:sqm' });
      out.push({ key: 'adguard', title: 'AdGuard DNS', status: adOn ? 'ON' : 'OFF', subtitle: 'DNS 过滤 · 去广告', tone: 'blue', active: adOn, toggleable: true, on: adOn, action: 'toggle:AdGuardHome' });
      out.push({ key: 'proxy', title: 'Smart Proxy', status: proxyOn ? 'ON' : 'OFF', subtitle: '透明代理 · 分流规则', tone: 'blue', active: proxyOn, toggleable: true, on: proxyOn, action: 'toggle:passwall' });
      out.push({ key: 'wan', title: 'WAN 链路', status: wanUp ? 'UP' : 'DOWN', subtitle: 'pppoe · wan', tone: wanUp ? 'green' : 'amber', active: wanUp, toggleable: false, on: wanUp, action: 'section:network' });
      out.push({ key: 'firewall', title: '防火墙', status: fullcone ? 'FULLCONE' : 'STD', subtitle: pfs.length + ' 端口映射 · ' + clients.filter((c) => c.blocked).length + ' 黑名单', tone: 'blue', active: fullcone, toggleable: false, on: fullcone, action: 'section:portforward' });
      out.push({ key: 'dhcp', title: 'DHCP 租约', status: 'ON', subtitle: clients.filter((c) => c.online).length + ' 台在线 · ' + clients.filter((c) => c.static).length + ' 静态', tone: 'green', active: true, toggleable: false, on: true, action: 'clients' });
      const core = out.filter((t) => t.key === 'wan' || t.key === 'firewall' || t.key === 'dhcp');
      const optional = out.filter((t) => core.indexOf(t) < 0);
      return optional.slice(0, Math.max(0, 8 - core.length)).concat(core);
    }

    function ports() {
      const online = clients.filter((c) => c.online && !c.blocked).length;
      return [
        { key: 'wan', label: 'WAN', up: wanUp, state: wanUp ? 'UP' : 'DOWN', detail: 'pppoe · wan', speed: wanUp ? '1000M FULL' : '' },
        { key: 'wan6', label: 'WAN6', up: wanUp, state: wanUp ? 'UP' : 'DOWN', detail: 'IPv6 · wan6' },
        { key: 'lan', label: 'LAN', up: true, state: 'UP', detail: 'br-lan · ' + online + ' 台在线', speed: '1000M FULL', clients: online },
        { key: 'cell', label: '4G/5G', up: true, state: 'UP', detail: 'CHINA MOBILE · usb0' },
      ];
    }

    function snapshot() {
      if (!connected) {
        return { connected: false, refreshedAt: Date.now(), clients: [], tiles: [], caps: {} };
      }
      const t = Date.now();
      const wave = (Math.sin((t - t0) / 2600) + 1) / 2;
      const elapsed = Math.floor((t - t0) / 1000);
      const rx = wanUp ? (69 + wave * 8 + Math.sin(t / 700) * 1.4) * 1048576 : 0;
      const tx = wanUp ? (12 + wave * 4) * 1048576 : 0;
      const cs = clients.map((c, i) => ({
        name: c.name, ip: c.ip, mac: c.mac, type: c.type, online: c.online,
        leaseRemaining: c.static ? -1 : Math.max(0, 86400 - ((elapsed + i * 600) % 86400)),
        expiresAt: startedAt + 86400, static: c.static, blocked: c.blocked,
        rxBps: c.online && !c.blocked ? c.rxb * 1048576 * (0.8 + wave * 0.4) : 0,
        txBps: c.online && !c.blocked ? c.txb * 1048576 * (0.8 + wave * 0.4) : 0,
        signal: -1,
      }));
      const s = {
        connected: true,
        refreshedAt: t,
        info,
        system: {
          hostname: 'OpenWrt-X86', model: 'ProLite Virtual Router', chipset: 'N100',
          kernel: '6.6.73', distribution: 'ImmortalWrt', release: 'ImmortalWrt 23.05-SNAPSHOT',
          uptime: 42 * 86400 + elapsed, cpuPct: 12 + wave * 9, cpuCores: 4,
          memPct: 29 + wave * 2, memUsed: 154 * 1048576, memTotal: 512 * 1048576,
          load1: 0.42 + wave * 0.2, load5: 0.36, load15: 0.31, processCount: 118,
          tempC: 43 + wave * 3, tempAvailable: true,
          flashFree: 1928 * 1024, flashTotal: 2240 * 1024,
        },
        wan: {
          up: wanUp, status: wanUp ? 'UP' : 'DOWN', proto: 'pppoe', device: 'wan',
          ip: wanUp ? '203.0.113.42' : '', gateway: wanUp ? '203.0.113.1' : '',
          dns, uptime: wanUp ? 42 * 86400 : 0, linkSpeed: '1000M FULL',
        },
        rates: { wanRx: rx, wanTx: tx, lanRx: rx * 0.32, lanTx: tx * 0.7, latencyMs: wanUp ? 11 + wave * 1.5 : 0, latencyOk: wanUp },
        clients: cs,
        tiles: tiles(),
        ports: ports(),
        plugins: pluginSummaries(),
        caps,
      };
      return s;
    }

    function lab() {
      return {
        info, caps,
        wan: snapshot().wan,
        portForwards: pfs.map((p) => ({ ...p })),
        dns: { servers: dns.slice(), effective: ['192.168.100.1'], doh: false },
        ddns: {
          installed: true,
          services: [{ name: 'cloudflare', domain: 'home.example.com', enabled: true, running: true }],
        },
        fullcone,
        services: services.map((s) => ({ ...s, running: s.key === 'sqm' ? sqmOn : s.key === 'AdGuardHome' ? adOn : s.running })),
        cellular: {
          present: true, installed: true, internet: true, connected: true, count: 1,
          mode: '4G', operator: 'CHINA MOBILE', model: 'RM500U-CNV', simStatus: 'READY',
          band: 'B3', rsrp: -78, sinr: 12, interface: 'usb0', ipv4: '192.168.4.170/24',
          gateway: '192.168.4.1', uptime: 7200, raw: '', devices: ['usb0'],
        },
        mwan: { installed: false, running: false, interfaces: [] },
        blocked: blocked.map((m) => ({ mac: m, section: 'rule_' + m })),
        wireless: { present: true, enabled: wifiOn, ssid: 'ProLite-5G', channel: '149', radios: ['radio0'] },
        fingerprint: cfg.fingerprint,
      };
    }

    let upnpOn = false;

    function pluginList() {
      return [
        { key: 'sqm', name: 'SQM QoS', version: '2025.06.28~1', package: 'sqm-scripts', config: 'sqm', init: 'sqm', installed: true, enabled: sqmOn, running: sqmOn },
        { key: 'adguard', name: 'AdGuard Home', version: '0.107.52', package: 'adguardhome', config: 'AdGuardHome', init: 'AdGuardHome', installed: true, enabled: adOn, running: adOn },
        { key: 'proxy', name: 'PassWall', version: '4.78', package: 'luci-app-passwall', config: 'passwall', init: 'passwall', installed: true, enabled: proxyOn, running: proxyOn },
        { key: 'doh', name: 'DoH 代理', version: '2025.05.16', package: 'https-dns-proxy', config: 'https-dns-proxy', init: 'https-dns-proxy', installed: true, enabled: false, running: false },
        { key: 'mwan3', name: 'MWAN3 多线', version: '2.11.14', package: 'mwan3', config: 'mwan3', init: 'mwan3', installed: true, enabled: false, running: false },
        { key: 'ttyd', name: 'Web 终端', version: '1.7.7', package: 'ttyd', config: 'ttyd', init: 'ttyd', installed: true, enabled: true, running: true },
        { key: 'uhttpd', name: 'uHTTPd', version: '2025.10.03', package: 'uhttpd', config: 'uhttpd', init: 'uhttpd', installed: true, enabled: true, running: true },
      ];
    }

    function pluginSummaries() {
      return pluginList().map((p) => ({ key: p.key, name: p.name, running: p.running, enabled: p.enabled }));
    }

    function consoleData() {
      const memTotal = 512 * 1048576;
      const used = 148 * 1048576;
      const ifUpWan = wanUp;
      return {
        board: { kernel: '6.6.73', hostname: 'OpenWrt-X86', system: 'x86_64', model: 'ProLite Virtual Router', board_name: 'x86_64', rootfs_type: 'ext4', release: { distribution: 'ImmortalWrt', version: '23.05-SNAPSHOT', description: 'ImmortalWrt 23.05-SNAPSHOT' } },
        release: { distribution: 'ImmortalWrt', version: '23.05-SNAPSHOT', description: 'ImmortalWrt 23.05-SNAPSHOT' },
        uname: 'Linux OpenWrt-X86 6.6.73 #0 SMP x86_64 GNU/Linux',
        hostname: info.hostname, cpuModel: 'Intel N100', cpuCores: 4, cpuFreqMhz: 3400,
        arch: 'x86_64', kernel: '6.6.73', uptime: 42 * 86400 + 3620,
        date: new Date().toISOString().slice(0, 19).replace('T', ' ') + ' UTC',
        timezone: 'CST-8', zonename: 'Asia/Shanghai', systemSection: 'cfg01e48a', ntpSection: 'ntp',
        ntpEnabled: true, ntpServers: ['time.cloudflare.com', 'pool.ntp.org'],
        memory: { total: memTotal, free: memTotal - used, available: memTotal - used, buffers: 12 * 1048576, cached: 96 * 1048576, swapTotal: 0, swapFree: 0 },
        storage: [
          { filesystem: '/dev/root', size: '128M', used: '24M', available: '104M', usePct: '19%', mount: '/' },
          { filesystem: 'tmpfs', size: '256M', used: '1.2M', available: '255M', usePct: '0%', mount: '/tmp' },
        ],
        mounts: [
          { device: '/dev/root', mount: '/', fs: 'ext4', options: 'rw,noatime' },
          { device: 'tmpfs', mount: '/tmp', fs: 'tmpfs', options: 'rw,nosuid,nodev' },
        ],
        interfaces: [
          { name: '蜂窝网络', up: true, pending: false, available: true, proto: '4G', device: 'usb0', ips: ['192.168.4.170/24'], gateway: '192.168.4.1', dns, uptime: 7200, rxBytes: 1024 * 1048576 * 12, txBytes: 1024 * 1048576 * 3, linkSpeed: '', cellModem: true },
          { name: 'lan', up: true, pending: false, available: true, proto: 'static', device: 'br-lan', ips: ['192.168.100.1/24'], gateway: '', dns: [], uptime: 42 * 86400, rxBytes: 1024 * 1048576 * 84, txBytes: 1024 * 1048576 * 31, linkSpeed: '1000M FULL' },
          { name: 'wan', up: ifUpWan, pending: false, available: true, proto: 'pppoe', device: 'wan', ips: ifUpWan ? ['203.0.113.42/32'] : [], gateway: ifUpWan ? '203.0.113.1' : '', dns, uptime: ifUpWan ? 42 * 86400 : 0, rxBytes: 1024 * 1048576 * 132, txBytes: 1024 * 1048576 * 44, linkSpeed: '1000M FULL' },
          { name: 'wan6', up: false, pending: false, available: true, proto: 'dhcpv6', device: 'wan', ips: [], gateway: '', dns: [], uptime: 0, rxBytes: 0, txBytes: 0, linkSpeed: '' },
          { name: 'loopback', up: true, pending: false, available: true, proto: 'static', device: 'lo', ips: ['127.0.0.1/8'], gateway: '', dns: [], uptime: 42 * 86400, rxBytes: 0, txBytes: 0, linkSpeed: '' },
        ],
        modem: {
          present: true, installed: true, connected: true, count: 1,
          mode: '4G', operator: 'CHINA MOBILE', model: 'RM500U-CNV', simStatus: 'READY',
          band: 'B3', rsrp: -78, sinr: 12, interface: 'usb0', ipv4: '192.168.4.170/24',
          gateway: '192.168.4.1', uptime: 7200,
          raw: '{"cellular":{"present":true,"device":"usb0","model":"RM500U-CNV","sim_status":"READY","connection":"connected","radio":"LTE","band":"B3","lte_signal":-78,"signal_valid":true}}',
        },
        wireless: {
          present: false,
          radios: [],
          ifaces: [],
        },
        plugins: pluginList(),
        packages: (function () {
          const out = [];
          const names = ['base-files', 'busybox', 'dnsmasq', 'dropbear', 'firewall4', 'fstools', 'kernel', 'libc', 'libuci', 'logd', 'mtd', 'netifd', 'odhcpd-ipv6only', 'opkg', 'ppp', 'procd', 'ubox', 'ubus', 'uci', 'uclient-fetch', 'uhttpd', 'urandom-seed', 'urngd', 'wpad-basic-mbedtls', 'luci', 'luci-app-firewall', 'luci-app-package-manager', 'luci-base', 'luci-mod-admin-full', 'luci-mod-network', 'luci-mod-status', 'luci-mod-system', 'luci-proto-ipv6', 'luci-proto-ppp', 'sqm-scripts', 'luci-app-sqm', 'https-dns-proxy', 'luci-app-https-dns-proxy', 'mwan3', 'luci-app-mwan3', 'ttyd', 'luci-app-ttyd', 'adguardhome', 'luci-app-adguardhome', 'passwall', 'luci-app-passwall', 'xray-core', 'chinadns-ng', 'haproxy', 'curl', 'ca-bundle', 'openssl-util', 'htop', 'nano', 'wget-ssl'];
          names.forEach((n, i) => out.push({ name: n, version: (i % 3 === 0 ? '2025.' : '1.') + (10 + (i % 20)) + '.0-r' + (i % 5 + 1) }));
          return out;
        })(),
        packageCount: 58,
        processes: [
          { pid: 1, user: 'root', vsz: 2840, stat: 'S', command: '/sbin/procd' },
          { pid: 421, user: 'root', vsz: 1892, stat: 'S', command: '/sbin/ubusd' },
          { pid: 778, user: 'root', vsz: 4260, stat: 'S', command: '/usr/sbin/dnsmasq -C /var/etc/dnsmasq.conf' },
          { pid: 903, user: 'root', vsz: 3120, stat: 'S', command: '/usr/sbin/dropbear -F -P /var/run/dropbear.1.pid' },
          { pid: 1180, user: 'root', vsz: 9840, stat: 'S', command: '/usr/bin/AdGuardHome --no-check-update' },
          { pid: 1422, user: 'root', vsz: 12480, stat: 'S', command: '/usr/bin/passwall -c /etc/config/passwall' },
          { pid: 1770, user: 'root', vsz: 2200, stat: 'S', command: '/usr/sbin/uhttpd -f -h /www' },
          { pid: 2001, user: 'root', vsz: 1640, stat: 'S', command: '/usr/bin/ttyd -p 7681 login' },
          { pid: 2212, user: 'root', vsz: 980, stat: 'S', command: '/sbin/netifd' },
          { pid: 2480, user: 'root', vsz: 760, stat: 'S', command: 'ntpd -n -N -S /usr/sbin/ntpd-hotplug' },
        ],
        dhcp: {
          pools: [{ section: 'lan', start: '100', limit: '150', leaseTime: '12h', interface: 'lan', ignore: false }],
          staticHosts: clients.filter((c) => c.static).map((c) => ({ section: 'host_' + c.mac.replace(/:/g, ''), name: c.name, ip: c.ip, mac: c.mac })),
          leases: clients.map((c) => ({ hostname: c.name, ip: c.ip, mac: c.mac, expires: startedAt + 43200 })),
        },
        firewall: {
          zones: [
            { name: 'lan', networks: ['lan'], input: 'ACCEPT', output: 'ACCEPT', forward: 'ACCEPT', masq: false },
            { name: 'wan', networks: ['wan', 'wan6'], input: 'REJECT', output: 'ACCEPT', forward: 'REJECT', masq: true },
          ],
          ruleCount: 9, redirectCount: pfs.length, fullCone: fullcone,
          upnpInstalled: true, upnpRunning: upnpOn, upnpEnabled: upnpOn, upnpSection: 'upnpd', upnpConfigName: 'upnpd',
        },
        mwan: { installed: false, running: false, interfaces: [] },
        configs: ['dhcp', 'dropbear', 'firewall', 'fstab', 'luci', 'mwan3', 'network', 'openssh', 'rpcd', 'sqm', 'system', 'uhttpd', 'wireless'],
        fetchedAt: Date.now(),
      };
    }

    function configValues(name) {
      const map = {
        system: {
          cfg01e48a: { '.anonymous': true, '.type': 'system', '.name': 'cfg01e48a', hostname: info.hostname, timezone: 'CST-8', zonename: 'Asia/Shanghai', ttylogin: '0', log_size: '128' },
          ntp: { '.anonymous': false, '.type': 'timeserver', '.name': 'ntp', enabled: '1', enable_server: '0', server: ['time.cloudflare.com', 'pool.ntp.org'] },
        },
        network: {
          lan: { '.anonymous': false, '.type': 'interface', '.name': 'lan', proto: 'static', device: 'br-lan', ipaddr: '192.168.100.1', netmask: '255.255.255.0', ip6assign: '60' },
          wan: { '.anonymous': false, '.type': 'interface', '.name': 'wan', proto: 'pppoe', device: 'wan', username: 'broadband', password: '••••••', metric: '10' },
          wan6: { '.anonymous': false, '.type': 'interface', '.name': 'wan6', proto: 'dhcpv6', device: 'wan' },
          globals: { '.anonymous': false, '.type': 'globals', '.name': 'globals', ula_prefix: 'fd00:100:1::/48' },
        },
        dhcp: {
          cfg01411c: { '.anonymous': true, '.type': 'dnsmasq', '.name': 'cfg01411c', domainneeded: '1', boguspriv: '1', local: '/lan/', domain: 'lan', cachesize: '1000', leasefile: '/tmp/dhcp.leases' },
          lan: { '.anonymous': false, '.type': 'dhcp', '.name': 'lan', interface: 'lan', start: '100', limit: '150', leasetime: '12h' },
        },
        firewall: {
          defaults: { '.anonymous': false, '.type': 'defaults', '.name': 'defaults', input: 'REJECT', output: 'ACCEPT', forward: 'REJECT', syn_flood: '1', fullcone: '1' },
          lan: { '.anonymous': false, '.type': 'zone', '.name': 'lan', name: 'lan', input: 'ACCEPT', output: 'ACCEPT', forward: 'ACCEPT' },
          wan: { '.anonymous': false, '.type': 'zone', '.name': 'wan', name: 'wan', input: 'REJECT', output: 'ACCEPT', forward: 'REJECT', masq: '1', mtu_fix: '1' },
        },
        wireless: {
          radio0: { '.anonymous': false, '.type': 'wifi-device', '.name': 'radio0', path: 'pci', channel: '149', band: '5g', htmode: 'HE80', txpower: '20', disabled: wifiOn ? '0' : '1' },
          default_radio0: { '.anonymous': false, '.type': 'wifi-iface', '.name': 'default_radio0', device: 'radio0', network: 'lan', mode: 'ap', ssid: 'ProLite-5G', encryption: 'sae-mixed', key: '***', disabled: wifiOn ? '0' : '1' },
        },
        sqm: {
          wan: { '.anonymous': false, '.type': 'queue', '.name': 'wan', enabled: sqmOn ? '1' : '0', interface: 'wan', download: '1000000', upload: '500000', script: 'cake', qdisc: 'cake', linklayer: 'ethernet' },
        },
        mwan3: {
          globals: { '.anonymous': false, '.type': 'globals', '.name': 'globals', mmx_mask: '0x3F00', logging: '1' },
        },
      };
      return map[name] || {};
    }

    function probe(kind, target) {
      if (kind === 'trace') {
        return [
          'traceroute to ' + target + ', 12 hops max, 38 byte packets',
          ' 1  192.168.100.1  0.642 ms  0.511 ms  0.488 ms',
          ' 2  100.64.0.1  3.220 ms  3.104 ms  2.980 ms',
          ' 3  172.16.8.1  6.410 ms  6.201 ms  6.554 ms',
          ' 4  ' + target + '  10.802 ms  10.441 ms  11.033 ms',
        ].join('\n');
      }
      return [
        'PING ' + target + ' (' + target + '): 56 data bytes',
        '64 bytes from ' + target + ': seq=0 ttl=58 time=10.8 ms',
        '64 bytes from ' + target + ': seq=1 ttl=58 time=11.2 ms',
        '64 bytes from ' + target + ': seq=2 ttl=58 time=10.6 ms',
        '64 bytes from ' + target + ': seq=3 ttl=58 time=11.0 ms',
        '',
        '--- ' + target + ' ping statistics ---',
        '4 packets transmitted, 4 packets received, 0% packet loss',
        'round-trip min/avg/max = 10.6/10.9/11.2 ms',
      ].join('\n');
    }

    const mockJobs = {};
    let mockJobSeq = 0;

    return {
      GetConfig: async () => ({ ...cfg }),
      Connect: async (inp) => {
        await sleep(420);
        connected = true;
        cfg = {
          ...cfg, saved: !!(inp && inp.remember), host: inp.host, port: inp.port, user: inp.user,
          auth: inp.auth, keyPath: inp.keyPath || '',
        };
        return { ...info, host: inp.host, port: inp.port, user: inp.user };
      },
      Disconnect: async () => { connected = false; },
      GetSnapshot: async () => { await sleep(40); return snapshot(); },
      GetStorageDevices: async () => { await sleep(140); return mockStorageView(); },
      MountStorage: async (inp) => {
        await sleep(260);
        if (!inp || !inp.confirmed || inp.confirmText !== cfg.host) throw new Error('请先确认挂载操作');
        const d = (mockStorageDevices || []).find((x) => x.device === inp.device);
        if (!d) throw new Error('DEVICE_NOT_FOUND：未识别到该设备');
        if (d.protected) throw new Error('SYSTEM_DISK：' + (d.blockedReason || '系统运行盘，禁止操作'));
        if (d.mounted) throw new Error('MOUNT_BUSY：设备已挂载');
        if (!d.fsType) throw new Error('FS_UNKNOWN：未识别到文件系统，可能未格式化');
        d.mounted = true;
        d.mountPoint = inp.mountPoint || d.mountTarget;
        return mockStorageView();
      },
      UnmountStorage: async (inp) => {
        await sleep(220);
        if (!inp || !inp.confirmed || inp.confirmText !== cfg.host) throw new Error('请先确认卸载操作');
        const d = (mockStorageDevices || []).find((x) => x.device === inp.device);
        if (!d) throw new Error('DEVICE_NOT_FOUND：未识别到该设备');
        if (d.protected) throw new Error('SYSTEM_DISK：' + (d.blockedReason || '系统运行盘，禁止操作'));
        if (!d.mounted) throw new Error('NOT_MOUNTED：设备当前未挂载');
        d.mounted = false;
        d.mountPoint = '';
        return mockStorageView();
      },
      GetMarketIndex: async () => {
        await sleep(180);
        const defs = [
          ['luci-app-argon-config', 'Argon 主题配置', 'LuCI'],
          ['luci-app-attendedsysupgrade', '在线升级助手', '系统'],
          ['luci-app-ddns', '动态域名解析', '网络'],
          ['luci-app-firewall', '防火墙规则', '网络'],
          ['luci-app-sqm', '智能队列限速', '网络'],
          ['luci-app-mwan3', '多 WAN 负载均衡', '网络'],
          ['luci-app-openclash', 'OpenClash 代理', '服务'],
          ['luci-app-passwall', 'PassWall 代理', '服务'],
          ['luci-app-adguardhome', 'AdGuard Home 去广告', '服务'],
          ['luci-app-upnp', 'UPnP 端口映射', '网络'],
          ['luci-app-wol', '网络唤醒', '工具'],
          ['luci-app-nlbwmon', '带宽监控', '网络'],
          ['luci-app-statistics', '系统统计图表', '系统'],
          ['luci-app-ttyd', '网页终端', '工具'],
          ['luci-app-filetransfer', '文件传输', '工具'],
          ['luci-app-vnstat2', '流量统计', '网络'],
          ['luci-app-wifischedule', '无线定时开关', '无线'],
          ['luci-app-cpufreq', 'CPU 调频', '系统'],
          ['luci-app-temp-status', '温度状态', '系统'],
          ['luci-app-zerotier', 'ZeroTier 组网', '服务'],
          ['luci-app-wireguard', 'WireGuard VPN', '服务'],
          ['luci-app-https-dns-proxy', 'DoH 加密 DNS', '网络'],
          ['luci-app-minidlna', 'DLNA 媒体共享', '服务'],
          ['luci-app-samba4', 'Samba 文件共享', '服务'],
          ['luci-app-dockerman', 'Docker 管理', '服务'],
          ['luci-app-ksmbd', 'KSMBD 文件共享', '服务'],
          ['luci-app-netdata', 'Netdata 监控', '系统'],
          ['luci-app-crowdsec', 'CrowdSec 防护', '安全'],
          ['kmod-usb-net-rtl8152', 'USB RTL8152/RTL8153 ethernet driver', 'Kernel'],
          ['base-files', 'Base filesystem and default configuration', 'System'],
          ['libc', 'Standard C library', 'Libraries'],
          ['openssl-util', 'OpenSSL command line utilities', 'Utilities'],
        ];
        const packages = defs.map((d, i) => ({
          name: d[0],
          version: '1.' + (i % 9) + '.' + (i % 5) + '-r1',
          size: 24000 + (i * 7919) % 480000,
          installed: i % 5 === 0,
          description: d[1],
          section: d[2],
          // 模拟后端翻译缓存：前 28 个已有中文说明，最后 4 个仅英文、显示“待翻译”
          zh: i < defs.length - 4 ? d[1] : '',
          origin: 'feeds',
        }));
        return { connected: true, packages, total: packages.length, sources: [{ url: 'device:feeds', count: packages.length }], elapsedMs: 142 };
      },
      StartInstallJob: async (req) => {
        await sleep(220);
        const pkg = (req && req.package) || 'luci-app-demo';
        const id = 'mock-' + (++mockJobSeq);
        const dry = !req || req.dryRun !== false;
        mockJobs[id] = {
          id,
          phase: 'done',
          percent: 100,
          dryRun: dry,
          running: false,
          plan: {
            packages: [{ name: pkg, version: '1.0.0-r1', arch: 'all', size: 180000, installedSize: 420000, isRoot: true, installed: false, status: 'pending', reason: '目标' }],
            totalSize: 180000,
            totalInstall: 420000,
            warnings: [],
          },
          checks: [
            { id: 'SPACE-OVERLAY', stage: 'A', label: 'overlay 空间充足', level: 'ok', detail: '可用 48 MB，预计占用 420 KB' },
            { id: 'MARKET-LIMITS', stage: 'A', label: '市场约束通过', level: 'ok', detail: '1 个待安装包，全部为 noarch 且未超体积上限' },
          ],
          files: [{ name: pkg + '.apk', size: 180000, sent: 180000, status: 'uploaded' }],
          log: ['解析设备包源…', '依赖闭包 1 个包', '市场约束通过', '下载完成', 'SHA256 校验通过', '上传 /tmp 完成', 'simulate 通过'],
          result: dry
            ? { success: true, code: 'DRY_RUN_OK', message: '预演通过，未写入设备' }
            : { success: true, code: 'INSTALL_OK', message: '安装完成，共 1 个包' },
        };
        return id;
      },
      GetInstallJob: async (id) => mockJobs[id] || null,
      CancelInstallJob: async () => {},
      GetDeviceWritePolicy: async () => ({
        configured: true, path: 'C:\\Users\\you\\AppData\\Roaming\\ProLite\\device-write-allow.json',
        host: (state.info && state.info.host) || '', allowOverlay: true, allowTmp: true, allowDangerous: true, matches: true, error: '',
      }),
      GetLab: async () => { await sleep(180); return lab(); },
      SetAlwaysOnTop: async () => {},
      CloseApp: async () => { toast('桌面应用内此按钮会关闭窗口', 'info'); },
      WindowMinimise: async () => {},
      WindowToggleMaximise: async () => {},
      WindowIsMaximised: async () => document.body.classList.contains('win-max'),
      PickKeyFile: async () => 'C:\\Users\\you\\.ssh\\id_ed25519',
      ResetFingerprint: async () => { cfg.fingerprint = ''; },
      CopyText: async (text) => {
        try { await navigator.clipboard.writeText(text); } catch (e) {
          const ta = document.createElement('textarea');
          ta.value = text; document.body.appendChild(ta); ta.select();
          document.execCommand('copy'); ta.remove();
        }
      },
      AddPortForward: async (inp) => {
        await sleep(260);
        pfs.push({ id: 'pf' + Date.now(), name: inp.name || '转发 ' + inp.extPort, proto: inp.proto, extPort: inp.extPort, destIp: inp.destIp, destPort: inp.destPort, enabled: true });
      },
      TogglePortForward: async (id, on) => {
        await sleep(180);
        const p = pfs.find((x) => x.id === id);
        if (p) p.enabled = !!on;
      },
      DeletePortForward: async (id) => { await sleep(180); pfs = pfs.filter((x) => x.id !== id); },
      SetStaticLease: async (inp) => {
        await sleep(240);
        const c = clients.find((x) => x.mac === inp.mac);
        if (c) {
          c.static = !!inp.enabled;
          if (inp.enabled && inp.ip) c.ip = inp.ip;
          if (inp.enabled && inp.name) c.name = inp.name;
        }
      },
      SetBlocked: async (mac, on) => {
        await sleep(220);
        const c = clients.find((x) => x.mac === mac);
        if (c) c.blocked = !!on;
        blocked = blocked.filter((m) => m !== mac);
        if (on) blocked.push(mac);
      },
      SetDNS: async (servers) => {
        await sleep(260);
        dns = String(servers).split(/[,;\s]+/).map((s) => s.trim()).filter(Boolean);
      },
      SetFullCone: async (on) => { await sleep(180); fullcone = !!on; },
      ToggleService: async (name, on) => {
        await sleep(280);
        const s = services.find((x) => x.key === name);
        if (s) { s.running = !!on; s.enabled = !!on; }
        if (name === 'sqm') sqmOn = !!on;
        if (name === 'AdGuardHome') adOn = !!on;
        if (name === 'passwall') proxyOn = !!on;
      },
      WanAction: async (action) => { await sleep(200); wanUp = action !== 'down'; },
      ReloadNetwork: async () => { await sleep(400); },
      RebootRouter: async () => { await sleep(300); connected = false; },
      RunProbe: async (kind, target) => {
        await sleep(900);
        return {
          command: kind === 'trace' ? 'traceroute -n -m 12 -q 1 -w 2 ' + target : 'ping -c 4 -W 2 ' + target,
          output: probe(kind, target), exitCode: 0, durationMs: kind === 'trace' ? 4200 : 3100,
        };
      },
      ExecCommand: async (cmd) => {
        await sleep(500);
        let out = '';
        let code = 0;
        if (cmd.indexOf('uname') === 0) out = 'Linux OpenWrt-X86 6.6.73 #0 SMP x86_64 GNU/Linux';
        else if (cmd.indexOf('df') === 0) out = 'Filesystem           1K-blocks      Used Available Use% Mounted on\n/dev/root               126976     30720     96256  24% /\ntmpfs                   256000      1204    254796   0% /tmp';
        else if (cmd.indexOf('logread') === 0) out = 'Wed Nov 19 00:41:02 2026 kern.info kernel: [    0.000000] Linux version 6.6.73\nWed Nov 19 00:41:03 2026 daemon.info dnsmasq[2211]: started, version 2.90 cachesize 1000';
        else if (cmd.indexOf('uci') === 0) out = 'network.wan=interface\nnetwork.wan.proto=\'pppoe\'\nnetwork.wan.device=\'wan\'';
        else { out = 'sh: ' + cmd.split(' ')[0] + ': not found'; code = 127; }
        return { command: cmd, output: out, exitCode: code, durationMs: 180 };
      },
      GetConsole: async () => { await sleep(260); return consoleData(); },
      GetLogs: async (kind, filter, lines) => {
        await sleep(240);
        const rows = [];
        const now = Date.now();
        for (let i = 0; i < Math.min(lines || 80, 80); i++) {
          const t = new Date(now - (80 - i) * 9000).toTimeString().slice(0, 8);
          const line = kind === 'kernel'
            ? '[  ' + (i * 0.421).toFixed(6) + '] ' + ['mt7915e: firmware ready', 'br-lan: port 1 link up 1000Mbps', 'IPv6: ADDRCONF(NETDEV_CHANGE)', 'pppoe-wan: link up'][i % 4]
            : t + ' daemon.info ' + ['dnsmasq[2211]: reply example.com is 93.184.216.34', 'netifd: wan (2211): lease obtained', 'dropbear[903]: child connection from 192.168.100.20:52144', 'AdGuardHome[1180]: query A example.com'][i % 4];
          if (!filter || line.toLowerCase().indexOf(filter.toLowerCase()) >= 0) rows.push(line);
        }
        return rows.join('\n');
      },
      GetConfigs: async () => consoleData().configs.slice(),
      GetConfigValues: async (name) => configValues(name),
      SetUCIOption: async () => { await sleep(220); },
      SetUCIList: async () => { await sleep(220); },
      DeleteUCIOption: async () => { await sleep(200); },
      SetSystem: async (inp) => {
        await sleep(320);
        if (inp && inp.hostname) { info.hostname = inp.hostname; }
      },
      SetWiFi: async () => { await sleep(420); },
      SetDHCPPool: async () => { await sleep(420); },
      SetUPnP: async (on) => { await sleep(280); upnpOn = !!on; },
      InterfaceAction: async (name, action) => {
        await sleep(320);
        if (name === 'wan') wanUp = action !== 'down';
      },
      ReloadService: async () => { await sleep(360); },
    };
  }

  /* ================= connect view ================= */
  function setAuth(auth) {
    state.auth = auth;
    $$('#authSeg button').forEach((b) => b.classList.toggle('active', b.dataset.auth === auth));
    $('#pane-none').classList.toggle('hidden', auth !== 'none');
    $('#pane-agent').classList.toggle('hidden', auth !== 'agent');
    $('#pane-key').classList.toggle('hidden', auth !== 'key');
    $('#pane-password').classList.toggle('hidden', auth !== 'password');
  }

  function showConnectError(msg) {
    const box = $('#connectError');
    if (!msg) { box.classList.add('hidden'); box.textContent = ''; return; }
    box.classList.remove('hidden');
    box.textContent = msg;
  }

  function setConnectBusy(busy) {
    const btn = $('#connectBtn');
    btn.disabled = busy;
    btn.querySelector('.btn-label').textContent = busy ? '连接中…' : '连接路由器';
    btn.querySelector('.spin-icon').classList.toggle('hidden', !busy);
  }

  async function doConnect() {
    if (!state.api) {
      showConnectError('未检测到 Pro-Lite 桌面运行时。请运行桌面程序，或在浏览器中加上 ?mock=1 预览界面。');
      return;
    }
    const host = $('#hostInput').value.trim();
    const port = parseInt($('#portInput').value.trim(), 10);
    const user = $('#userInput').value.trim();
    if (!/^[A-Za-z0-9._:-]{1,253}$/.test(host) || host.startsWith('-')) {
      showConnectError('主机地址格式不正确。'); return;
    }
    if (!port || port < 1 || port > 65535) {
      showConnectError('SSH 端口必须在 1-65535 之间。'); return;
    }
    if (!/^[A-Za-z0-9._-]{1,32}$/.test(user)) {
      showConnectError('用户名格式不正确。'); return;
    }
    if (state.auth === 'key' && !$('#keyPathInput').value.trim()) {
      showConnectError('请选择 SSH 私钥文件。'); return;
    }
    if (state.auth === 'password' && !$('#passwordInput').value) {
      showConnectError('请输入 SSH 密码。'); return;
    }

    showConnectError('');
    setConnectBusy(true);
    try {
      const info = await state.api.Connect({
        host, port, user,
        auth: state.auth,
        keyPath: $('#keyPathInput').value.trim(),
        passphrase: $('#passphraseInput').value,
        password: $('#passwordInput').value,
        remember: $('#rememberInput').checked,
      });
      $('#passwordInput').value = '';
      state.lastConn = {
        host, port, user,
        auth: state.auth,
        keyPath: $('#keyPathInput').value.trim(),
        passphrase: $('#passphraseInput').value,
      };
      state.info = info;
      enterMain(info);
    } catch (e) {
      showConnectError(errText(e));
      $('#resetFpBtn').classList.toggle('hidden', !(state.cfg && (state.cfg.fingerprint || '').length));
    } finally {
      setConnectBusy(false);
    }
  }

  function fillConfig(cfg) {
    $('#hostInput').value = cfg.host || '192.168.100.1';
    $('#portInput').value = cfg.port || 22;
    $('#userInput').value = cfg.user || 'root';
    $('#keyPathInput').value = cfg.keyPath || '';
    $('#rememberInput').checked = cfg.saved !== false;
    setAuth(cfg.auth || 'none');
    $('#resetFpBtn').classList.toggle('hidden', !(cfg.fingerprint || '').length);
    const platform = cfg.platform === 'darwin' ? 'macOS' : cfg.platform === 'linux' ? 'Linux' : cfg.platform === 'win32' ? 'Windows' : (cfg.platform || 'desktop');
    $('#connectPlatform').textContent = platform;
    if (cfg.version) $('#connectVersion').textContent = 'v' + cfg.version;
    state.pinned = !!cfg.pinned;
  }

  function updateReconnectUI() {
    const can = !!state.lastConn;
    const mainBtn = $('#reconnectBtn');
    if (mainBtn) mainBtn.classList.toggle('hidden', !(state.linkDown && can));
    const connectBtn = $('#connectReconnectBtn');
    if (connectBtn) connectBtn.classList.toggle('hidden', !(!state.connected && can));
  }

  function setReconnectBusy(busy) {
    ['#reconnectBtn', '#connectReconnectBtn'].forEach((sel) => {
      const btn = $(sel);
      if (!btn) return;
      btn.disabled = busy;
      const label = btn.lastChild;
      if (label && label.nodeType === 3) label.nodeValue = busy ? ' 连接中…' : ' 重新连接';
    });
  }

  async function reconnectLast() {
    const lc = state.lastConn;
    if (!lc) { toast('没有可重连的设备记录', 'info'); return; }
    if (!state.api) {
      showConnectError('未检测到 Pro-Lite 桌面运行时。请运行桌面程序。');
      return;
    }
    if (lc.auth === 'password') {
      leaveToConnect('重新连接需要再次输入密码。');
      const pw = $('#passwordInput');
      if (pw) pw.focus();
      return;
    }
    fillConfig({
      host: lc.host, port: lc.port, user: lc.user, auth: lc.auth,
      keyPath: lc.keyPath, saved: true,
      fingerprint: (state.cfg && state.cfg.fingerprint) || '',
    });
    $('#passphraseInput').value = lc.passphrase || '';
    setReconnectBusy(true);
    try {
      const info = await state.api.Connect({
        host: lc.host, port: lc.port, user: lc.user, auth: lc.auth,
        keyPath: lc.keyPath || '', passphrase: lc.passphrase || '',
        password: '', remember: !!(state.cfg && state.cfg.saved),
      });
      state.info = info;
      enterMain(info);
      toast('已重新连接 ' + (info.hostname || info.host || lc.host), 'ok');
    } catch (e) {
      state.linkDown = true;
      updateReconnectUI();
      toastError(e);
    } finally {
      setReconnectBusy(false);
    }
  }

  /* ================= main entry ================= */
  function enterMain(info) {
    state.connected = true;
    state.linkDown = false;
    state.leaving = false;
    state.info = info || state.info;
    state.tileSig = '';
    state.clientSig = '';
    state.expandedMac = '';
    state.console = null;
    state.consoleError = '';
    state.consoleLoading = false;
    state.cTab = 'overview';
    state.cfgName = '';
    state.cfgList = [];
    state.cfgVals = null;
    state.logOut = '';
    $('#connectView').classList.add('hidden');
    $('#mainView').classList.remove('hidden');
    $('#connBanner').classList.add('hidden');
    const dot = $('#connDot');
    dot.className = 'status-dot';
    $('#hostName').textContent = (state.info && (state.info.hostname || state.info.host)) || 'Pro-Lite';
    refreshSnapshot(true);
    startPolling();
    updateReconnectUI();
  }

  function stopPolling() {
    state.pollGen += 1;
    if (state.pollTimer) { clearTimeout(state.pollTimer); state.pollTimer = null; }
  }

  function startPolling() {
    stopPolling();
    const gen = state.pollGen;
    let delay = 900;
    let failures = 0;
    const tick = async () => {
      if (!state.connected || gen !== state.pollGen) return;
      // 窗口隐藏/最小化时降低轮询频率，避免持续占用路由器的 SSH 与 shell 资源
      if (document.hidden) {
        state.pollTimer = setTimeout(tick, 4000);
        return;
      }
      if (state.pollBusy) { state.pollTimer = setTimeout(tick, 350); return; }
      state.pollBusy = true;
      let ok = true;
      try {
        const snap = await state.api.GetSnapshot();
        onSnapshot(snap);
        if (!snap || !snap.connected) ok = false;
      } catch (e) {
        ok = false;
        const now = Date.now();
        if (now - state.lastPollError > 8000) {
          state.lastPollError = now;
          toastError(e);
        }
        $('#connDot').className = 'status-dot err';
      } finally {
        state.pollBusy = false;
      }
      if (gen !== state.pollGen) return;
      failures = ok ? 0 : Math.min(failures + 1, 4);
      delay = ok ? 900 : Math.min(Math.round(900 * Math.pow(1.7, failures)), 6000);
      if (state.connected) state.pollTimer = setTimeout(tick, delay);
    };
    state.pollTimer = setTimeout(tick, 60);
  }

  async function refreshSnapshot(force) {
    if (!state.api || !state.connected) return;
    try {
      const snap = await state.api.GetSnapshot();
      onSnapshot(snap);
    } catch (e) {
      if (force) toastError(e);
    }
  }

  function onSnapshot(snap) {
    if (!snap) return;
    state.snap = snap;
    if (snap.info) state.info = snap.info;
    const dot = $('#connDot');
    const banner = $('#connBanner');
    if (!snap.connected) {
      dot.className = 'status-dot err';
      state.linkDown = true;
      const msg = snap.error || 'SSH 连接已断开，可点击右上角“重新连接”。';
      banner.textContent = msg;
      banner.classList.remove('hidden');
      updateReconnectUI();
      return;
    }
    dot.className = 'status-dot';
    state.linkDown = false;
    banner.classList.add('hidden');
    updateReconnectUI();
    renderSnapshot(snap);
  }

  /* ================= render: snapshot ================= */
  function renderSnapshot(snap) {
    const info = snap.info || state.info || {};
    const sys = snap.system || {};
    const wan = snap.wan || {};
    const rates = snap.rates || {};

    $('#hostName').textContent = info.hostname || info.host || 'Pro-Lite';
    $('#hostName').title = [info.model, info.release].filter(Boolean).join(' · ');
    const link = wan.linkSpeed || (wan.device ? String(wan.device).toUpperCase() : '—');
    $('#linkBadge').textContent = link;

    const proto = (wan.proto || 'wan').toUpperCase();
    $('#wanProto').textContent = proto;
    const st = $('#wanStatus');
    const up = !!wan.up;
    st.className = 'wan-status' + (up ? '' : wan.status === 'DOWN' || !wan.status ? ' down' : ' off');
    st.innerHTML = '<i></i>' + esc(wan.status || (up ? 'UP' : 'DOWN'));
    const ip = wan.ip || '';
    $('#wanIP').textContent = ip ? (state.ipMasked ? ip.replace(/\.\d+$/, '.•••') : ip) : '无地址';
    $('#copyIPBtn').style.visibility = ip ? 'visible' : 'hidden';
    $('#maskIPBtn').style.visibility = ip ? 'visible' : 'hidden';

    const lat = $('#latencyMetric');
    if (rates.latencyOk) {
      lat.className = 'metric' + (rates.latencyMs > 100 ? ' warn' : '');
      lat.innerHTML = '<i class="dot"></i>' + Math.round(rates.latencyMs) + ' ms';
    } else {
      lat.className = 'metric off';
      lat.innerHTML = '<i class="dot"></i>' + (up ? '-- ms' : '无链路');
    }
    const uptimeSec = wan.uptime || sys.uptime || 0;
    $('#uptimeMetric').textContent = uptimeSec ? 'UP ' + shortDur(uptimeSec) : 'UP —';

    $('#rxValue').innerHTML = rateHTML(rates.wanRx || 0);
    $('#txValue').innerHTML = rateHTML(rates.wanTx || 0);

    renderHardware(sys);
    renderDeviceCard(snap);
    renderTopology(snap);
    renderTiles(snap.tiles || []);
    renderClients(snap.clients || []);
  }

  function renderDeviceCard(snap) {
    const sys = snap.system || {};
    const info = snap.info || state.info || {};
    const flashTotal = Number(sys.flashTotal) || 0;
    const flashFree = Number(sys.flashFree) || 0;
    const flashLow = flashTotal > 0 && flashFree < 1024 * 1024;
    const cells = [
      ['型号', info.model || sys.model],
      ['芯片', sys.chipset],
      ['内核', sys.kernel],
      ['发行版', sys.release || sys.distribution],
      ['CPU', sys.cpuCores ? sys.cpuCores + ' 核' : ''],
      ['运行', sys.uptime ? shortDur(sys.uptime) : ''],
      ['温度', sys.tempAvailable ? Math.round(sys.tempC) + '°C' : ''],
      ['根分区', flashTotal ? fmtBytes(flashFree) + ' 空闲 · ' + fmtBytes(flashTotal) : '', flashLow],
    ].filter((x) => x[1]);
    $('#devSummary').innerHTML = '<div class="dev-grid">' + cells.map((x) =>
      '<div class="dev-cell' + (x[2] ? ' dev-warn' : '') + '"><span class="dev-label">' + esc(x[0]) + '</span><span class="dev-value mono">' + esc(x[1]) + '</span></div>'
    ).join('') + '</div>' +
      (flashLow
        ? '<div class="space-warn">根分区只剩 ' + esc(fmtBytes(flashFree)) + ' / ' + esc(fmtBytes(flashTotal)) +
          '，已接近写满。安装插件、保存配置或写入日志可能失败，请立即清理或扩容 overlay。</div>'
        : '');
    const plugins = snap.plugins || [];
    $('#pluginChips').innerHTML = plugins.length
      ? plugins.map((p) => '<span class="plugin-chip' + (p.running ? ' on' : '') + '" title="' + (p.running ? '运行中' : (p.enabled ? '已启用未运行' : '未启用')) + '">' +
          '<i></i>' + esc(p.name) + '</span>').join('')
      : '<span class="note">未识别到可管理的插件</span>';
  }

  function renderHardware(sys) {
    const cpu = cssNum(sys.cpuPct);
    const ram = cssNum(sys.memPct);
    $('#cpuValue').textContent = sys.cpuPct != null ? Math.round(cpu) + '%' : '--';
    $('#ramValue').textContent = sys.memPct != null ? Math.round(ram) + '%' : '--';
    setBar('#cpuBar', cpu, cpu >= 85 ? 'crit' : cpu >= 65 ? 'warn' : '');
    setBar('#ramBar', ram, ram >= 88 ? 'crit' : ram >= 75 ? 'warn' : '');
    $('#cpuSub').textContent = (sys.cpuCores || '--') + ' 核 · LOAD ' + (sys.load1 != null ? Number(sys.load1).toFixed(2) : '--');
    $('#ramSub').textContent = fmtBytes(sys.memUsed) + ' / ' + fmtBytes(sys.memTotal);

    if (sys.tempAvailable) {
      const temp = Number(sys.tempC) || 0;
      $('#tempValue').textContent = Math.round(temp) + '°C';
      const pct = cssNum((temp - 25) / 70 * 100);
      setBar('#tempBar', pct, temp >= 78 ? 'crit' : temp >= 65 ? 'warn' : '');
      $('#tempSub').textContent = 'SoC · ' + (temp >= 65 ? '偏高' : '正常');
    } else {
      $('#tempValue').textContent = '--';
      setBar('#tempBar', 0, '');
      $('#tempSub').textContent = '传感器不可用';
    }
  }

  function setBar(sel, pct, tone) {
    const bar = $(sel);
    if (!bar) return;
    bar.style.width = pct + '%';
    bar.className = tone;
  }

  function tileIcon(key) {
    const map = {
      wifi: 'wifi', cellular: 'cell', sqm: 'gauge', adguard: 'shield', proxy: 'proxy',
      mwan3: 'mwan', wan: 'wan', firewall: 'shield', dhcp: 'dhcp',
    };
    return map[key] || 'router';
  }

  function renderTiles(tiles) {
    const grid = $('#tilesGrid');
    const sig = JSON.stringify(tiles.map((t) => [t.key, t.status, t.subtitle, t.detail, t.active, t.on, t.action]));
    grid.className = 'tiles count-' + tiles.length;
    if (sig === state.tileSig) return;
    state.tileSig = sig;
    if (!tiles.length) {
      grid.innerHTML = '<div class="empty" style="grid-column:1/-1">正在读取策略状态…</div>';
      return;
    }
    grid.innerHTML = tiles.map((t) => {
      const tone = t.tone || 'muted';
      const foot = t.toggleable
        ? (t.on ? 'ON · 点击停用' : 'OFF · 点击启用')
        : t.action === 'clients' ? '管理终端 →'
        : t.action === 'cell:detail' ? '蜂窝详情 →'
        : t.action === 'section:portforward' ? '端口映射 →'
        : '链路详情 →';
      return '<button type="button" class="tile' + (t.active ? ' active' : '') +
        (t.toggleable ? ' switchable' : '') + (t.on ? ' on' : '') +
        '" data-tile="' + esc(t.key) + '">' +
        '<div class="tile-top"><span class="tile-icon"><svg class="i"><use href="#i-' + tileIcon(t.key) + '"/></svg></span>' +
        '<span class="tile-title">' + esc(t.title) + '</span>' +
        '<span class="tile-status ' + esc(tone) + '">' + esc(t.status) + '</span></div>' +
        '<div class="tile-sub">' + esc(t.subtitle) + '</div>' +
        (t.detail ? '<div class="tile-detail mono">' + esc(t.detail) + '</div>' : '') +
        '<div class="tile-foot">' + esc(foot) + '</div></button>';
    }).join('');
  }

  /* ================= clients ================= */
  function clientTypeLabel(c) {
    const t = c.type || 'LAN';
    if (/^LAN$/i.test(t)) return 'LAN';
    return t.replace(/\s*LAN$/i, '');
  }

  function clientIcon(c) {
    return /^(5g|2\.4g|wifi|wlan)/i.test(c.type || '') ? 'wifi' : 'router';
  }

  function clientLiveHTML(c) {
    const total = (c.rxBps || 0) + (c.txBps || 0);
    if (total > 0) {
      return '<span class="down">↓ ' + rateShort(c.rxBps) + '/s</span>' +
        '<span class="up">↑ ' + rateShort(c.txBps) + '/s</span>';
    }
    if (c.static) return '<span>静态</span>';
    if (c.leaseRemaining > 0) return '<span>' + shortDur(c.leaseRemaining) + '</span>';
    return '<span>' + (c.online ? '在线' : '离线') + '</span>';
  }

  function clientsSignature(clients) {
    return clients.map((c) => [c.mac, c.name, c.ip, c.type, c.online, c.static, c.blocked].join('|')).join(';');
  }

  function renderClients(clients, force) {
    const list = $('#clientList');
    $('#clientCount').textContent = clients.filter((c) => c.online).length + '/' + clients.length;
    const sig = clientsSignature(clients) + '|open:' + state.expandedMac;
    if (force || sig !== state.clientSig) {
      state.clientSig = sig;
      if (!clients.length) {
        list.innerHTML = '<div class="empty">暂无 DHCP 租约或 ARP 记录</div>';
      } else {
        list.innerHTML = clients.map((c) => clientRowHTML(c)).join('');
      }
    }
    clients.forEach((c) => {
      const el = list.querySelector('[data-live="' + c.mac + '"]');
      if (el) el.innerHTML = clientLiveHTML(c);
      const lease = list.querySelector('[data-lease="' + c.mac + '"]');
      if (lease) lease.textContent = leaseText(c);
    });
  }

  function clientRowHTML(c) {
    const open = state.expandedMac === c.mac;
    const tags = [];
    if (c.static) tags.push('<span class="client-tag static">静态</span>');
    if (c.blocked) tags.push('<span class="client-tag blocked">拉黑</span>');
    if (!c.online) tags.push('<span class="client-tag offline">离线</span>');
    return '<div class="client-row' + (open ? ' open' : '') + '" data-mac="' + esc(c.mac) + '">' +
      '<div class="client-main" data-client="' + esc(c.mac) + '">' +
        '<span class="client-ico ' + (clientIcon(c) === 'wifi' ? 'wifi' : '') + '"><svg class="i"><use href="#i-' + clientIcon(c) + '"/></svg></span>' +
        '<div class="client-meta"><div class="client-name">' + esc(c.name) + '</div>' +
        '<div class="client-sub"><span>' + esc(c.ip) + '</span><span class="client-tag">' + esc(clientTypeLabel(c)) + '</span>' +
        tags.join('') + '</div></div>' +
        '<div class="client-rate" data-live="' + esc(c.mac) + '">' + clientLiveHTML(c) + '</div>' +
        '<span class="client-caret"><svg class="i"><use href="#i-chevron"/></svg></span>' +
      '</div>' +
      (open ? clientDrawerHTML(c) : '') +
    '</div>';
  }

  function clientDrawerHTML(c) {
    return '<div class="client-drawer">' +
      '<div class="kv"><span><b>MAC</b> ' + esc(c.mac) + '</span>' +
      '<span><b>接入</b> ' + esc(clientTypeLabel(c)) + '</span>' +
      '<span><b>租约</b> <span data-lease="' + esc(c.mac) + '">' + esc(leaseText(c)) + '</span></span>' +
      (c.blocked ? '<span><b>状态</b> 已拉黑</span>' : '') + '</div>' +

      '<div class="subblock">' +
        '<div class="subblock-title">静态 DHCP 租约 ' + (c.static ? '<span class="pill">已绑定</span>' : '') + '</div>' +
        '<div class="row">' +
          '<input class="lease-name" placeholder="主机名" value="' + esc(c.static ? c.name : '') + '" spellcheck="false">' +
          '<input class="lease-ip" placeholder="IP 地址" value="' + esc(c.ip) + '" spellcheck="false">' +
        '</div>' +
        '<div class="row" style="justify-content:flex-end">' +
          '<button type="button" class="btn mini" data-cact="static-save">' + (c.static ? '更新租约' : '绑定静态 IP') + '</button>' +
          (c.static ? '<button type="button" class="btn mini ghost" data-cact="static-release">释放</button>' : '') +
        '</div>' +
      '</div>' +

      '<div class="subblock">' +
        '<div class="subblock-title">带宽限速 <span class="pill">Mbps</span></div>' +
        '<div class="range-row"><input type="range" min="1" max="1000" value="100" disabled><span class="val">固件未提供</span></div>' +
        '<div class="note">当前固件没有 per-client 流量整形接口；限速需在路由器侧用 tc / SQM 配置。</div>' +
      '</div>' +

      '<div class="row">' +
        '<button type="button" class="btn mini ' + (c.blocked ? '' : 'danger') + '" data-cact="' + (c.blocked ? 'unblock' : 'block') + '" data-label="' + (c.blocked ? '解除拉黑' : '断开并拉黑') + '">' + (c.blocked ? '解除拉黑' : '断开并拉黑') + '</button>' +
        '<span class="note">通过 firewall 规则按 MAC 丢弃转发流量。</span>' +
      '</div>' +
    '</div>';
  }

  async function handleClientAction(btn) {
    const row = btn.closest('.client-row');
    const mac = row && row.dataset.mac;
    const c = (state.snap && state.snap.clients || []).find((x) => x.mac === mac);
    if (!c) return;
    const act = btn.dataset.cact;
    const run = async (fn, ok) => {
      row.classList.add('busy');
      try {
        await fn();
        toast(ok, 'ok', 2000);
        await refreshSnapshot(true);
        if (state.labOpen) refreshLab();
      } catch (e) {
        toastError(e);
      } finally {
        row.classList.remove('busy');
      }
    };
    if (act === 'static-save') {
      const name = row.querySelector('.lease-name').value.trim();
      const ip = row.querySelector('.lease-ip').value.trim();
      run(() => state.api.SetStaticLease({ mac, ip, name, enabled: true }), '静态租约已保存');
    } else if (act === 'static-release') {
      run(() => state.api.SetStaticLease({ mac, ip: c.ip, name: c.name, enabled: false }), '静态租约已释放');
    } else if (act === 'unblock') {
      run(() => state.api.SetBlocked(mac, false), '已解除拉黑');
    } else if (act === 'block') {
      arm(btn, btn.dataset.label || '断开并拉黑', () => {
        run(() => state.api.SetBlocked(mac, true), '已拉黑该设备');
      });
    }
  }

  function toggleClient(mac) {
    state.expandedMac = state.expandedMac === mac ? '' : mac;
    renderClients(state.snap ? state.snap.clients || [] : [], true);
  }

  /* ================= topology ================= */
  function renderTopology(snap) {
    const host = $('#topoWrap');
    if (!host) return;
    const ports = (snap && snap.ports) || [];
    const info = (snap && snap.info) || state.info || {};
    const sys = (snap && snap.system) || {};
    const hostName = info.hostname || info.host || 'Pro-Lite';
    const sub = [info.distribution, sys.cpuCores ? sys.cpuCores + ' 核' : ''].filter(Boolean).join(' · ');
    const sig = hostName + '|' + sub + '|' + ports.map((p) => [p.key, p.up, p.state, p.detail].join('|')).join(';');
    if (host.dataset.sig === sig && host.firstChild) return;
    host.dataset.sig = sig;

    const slots = [
      { x: 2, y: 2, link: 'M108 22 C126 22 136 32 146 44' },
      { x: 2, y: 90, link: 'M108 110 C126 110 136 96 148 78' },
      { x: 290, y: 2, link: 'M288 22 C270 22 260 32 250 44' },
      { x: 290, y: 90, link: 'M288 110 C270 110 260 96 248 78' },
      { x: 146, y: 140, link: 'M198 138 L198 110' },
    ];
    const height = ports.length > 4 ? 182 : 140;
    const clip = (s, n) => {
      const v = String(s == null ? '' : s);
      return v.length > n ? v.slice(0, n - 1) + '…' : v;
    };
    const toneOf = (p) => p.up ? 'up' : (p.state === '无硬件' || p.state === '未配置' || p.state === '无模组') ? 'none' : 'off';

    const parts = [];
    ports.forEach((p, i) => {
      const slot = slots[i];
      if (!slot) return;
      parts.push('<path class="tp-link ' + toneOf(p) + '" d="' + slot.link + '"/>');
    });

    parts.push(
      '<ellipse class="tp-shadow" cx="198" cy="118" rx="96" ry="13"/>',
      '<g class="tp-device">',
      '<polygon class="tp-face-top" points="198,18 282,50 198,82 114,50"/>',
      '<polygon class="tp-face-left" points="114,50 198,82 198,108 114,76"/>',
      '<polygon class="tp-face-right" points="198,82 282,50 282,76 198,108"/>',
      '<polyline class="tp-vent" points="224,92 268,72"/><polyline class="tp-vent" points="230,98 274,78"/><polyline class="tp-vent" points="236,104 280,84"/>',
      '<text class="tp-host" x="198" y="49" text-anchor="middle">' + esc(clip(hostName, 16)) + '</text>',
      '<text class="tp-sub" x="198" y="63" text-anchor="middle">' + esc(clip(sub, 20)) + '</text>',
      '<circle class="tp-led" cx="132" cy="63" r="2.2"/>',
      '<circle class="tp-led" cx="148" cy="69" r="2.2"/>',
      '<circle class="tp-led" cx="164" cy="75" r="2.2"/>',
      '<circle class="tp-led" cx="180" cy="81" r="2.2"/>',
      '</g>'
    );

    ports.forEach((p, i) => {
      const slot = slots[i];
      if (!slot) return;
      const tone = toneOf(p);
      const detail = clip(p.detail || '', 21);
      const state = clip(p.state || (p.up ? 'UP' : 'DOWN'), 8);
      const title = p.label + ' · ' + state + (detail ? ' · ' + detail : '');
      parts.push(
        '<g class="tp-port ' + tone + '" transform="translate(' + slot.x + ',' + slot.y + ')" data-port="' + esc(p.key) + '">',
        '<title>' + esc(title) + '</title>',
        '<rect class="tp-chip" width="104" height="40" rx="8"/>',
        '<circle class="tp-dot" cx="10" cy="13" r="3.2"/>',
        '<text class="tp-label" x="19" y="16">' + esc(p.label) + '</text>',
        '<text class="tp-state" x="96" y="16" text-anchor="end">' + esc(state) + '</text>',
        '<text class="tp-detail" x="10" y="31">' + esc(detail || '—') + '</text>',
        '</g>'
      );
    });

    host.innerHTML = '<svg class="tp-svg" viewBox="0 0 396 ' + height + '" preserveAspectRatio="xMidYMid meet">' + parts.join('') + '</svg>';
  }

  /* ================= tiles ================= */
  async function handleTile(btn) {
    const key = btn.dataset.tile;
    const tile = (state.snap && state.snap.tiles || []).find((t) => t.key === key);
    if (!tile) return;
    const action = tile.action || '';
    if (action.indexOf('toggle:') === 0) {
      const name = action.slice(7);
      btn.classList.add('busy');
      try {
        await state.api.ToggleService(name, !tile.on);
        toast((tile.title || name) + (tile.on ? ' 已停用' : ' 已启用'), 'ok', 2200);
        await refreshSnapshot(true);
        if (state.labOpen) refreshLab();
      } catch (e) {
        toastError(e);
      } finally {
        btn.classList.remove('busy');
      }
      return;
    }
    if (action === 'clients') {
      const card = $('#clientsCard');
      card.scrollIntoView({ behavior: 'smooth', block: 'center' });
      card.classList.remove('flash');
      void card.offsetWidth;
      card.classList.add('flash');
      return;
    }
    if (action === 'cell:detail') {
      openCellView();
      return;
    }
    if (action.indexOf('section:') === 0) {
      const which = action.slice(8);
      if (which === 'portforward') { state.accOpen.add('pf'); openLab('lab-pf', 'tools'); }
      else { state.accOpen.add('link'); openLab('lab-link', 'tools'); }
    }
  }

  /* ================= lab ================= */
  async function openLab(focus, tab) {
    if (!state.connected && tab !== 'market') return;
    state.labOpen = true;
    if (tab) state.labTab = tab;
    const view = $('#labView');
    view.classList.add('open');
    view.setAttribute('aria-hidden', 'false');
    renderLab();
    if (state.labTab === 'market') { ensureMarketRepos(); ensureMarketIndex(); loadMarketPolicy(); return; }
    if (state.labTab === 'tools') await fetchLab();
    else await fetchConsole();
    if (focus) scrollLab(focus);
  }

  function closeLab() {
    state.labOpen = false;
    const view = $('#labView');
    view.classList.remove('open');
    view.setAttribute('aria-hidden', 'true');
  }

  async function fetchLab() {
    if (!state.api || !state.connected) return;
    state.labLoading = true;
    renderLab();
    try {
      state.lab = await state.api.GetLab(true);
      if (state.lab && state.lab.fingerprint && state.cfg) state.cfg.fingerprint = state.lab.fingerprint;
    } catch (e) {
      toastError(e);
      if (!state.lab) state.lab = null;
    } finally {
      state.labLoading = false;
      renderLab();
    }
  }

  function refreshLab() {
    if (!state.labOpen) return;
    if (state.labTab === 'tools') fetchLab();
    else if (state.labTab === 'market') renderMarket();
    else fetchConsole(true);
  }

  async function fetchConsole(force) {
    if (!state.api || !state.connected) return;
    state.consoleLoading = true;
    renderLab();
    try {
      const c = await state.api.GetConsole(!!force);
      state.console = c;
      state.consoleError = '';
      if (c) {
        const list = c.configs || [];
        state.cfgList = list.slice();
        if (!state.cfgName || list.indexOf(state.cfgName) < 0) {
          state.cfgName = list.indexOf('network') >= 0 ? 'network' : (list[0] || '');
          state.cfgVals = null;
        }
      }
    } catch (e) {
      state.consoleError = errText(e);
      if (!state.console) toastError(e);
    } finally {
      state.consoleLoading = false;
      renderLab();
      if (state.cTab === 'config' && !state.cfgVals) ensureConfigData();
    }
  }

  function openCellView() {
    if (!state.api || !state.connected) return;
    $('#mainView').classList.add('hidden');
    $('#cellView').classList.remove('hidden');
    $('#cellView').setAttribute('aria-hidden', 'false');
    renderCellView();
    loadCellData(false);
  }

  function closeCellView() {
    $('#cellView').classList.add('hidden');
    $('#cellView').setAttribute('aria-hidden', 'true');
    if (state.connected) $('#mainView').classList.remove('hidden');
  }

  async function loadCellData(force) {
    if (!state.api || !state.connected) return;
    state.cellLoading = true;
    renderCellView();
    try {
      state.console = await state.api.GetConsole(!!force);
      state.cellError = '';
    } catch (e) {
      state.cellError = errText(e);
      if (state.cellError.indexOf('需要重新输入密码') >= 0) toastError(e);
    } finally {
      state.cellLoading = false;
      renderCellView();
    }
  }

  function simLabel(s) {
    const u = String(s || '').trim().toUpperCase();
    if (!u) return '—';
    if (/ABSENT|NOT INSERTED|NO SIM|MISS/.test(u)) return '未插卡';
    if (/ERROR|FAIL/.test(u)) return '卡异常';
    if (/PIN|LOCK/.test(u)) return '已锁定';
    if (/READY|OK|NORMAL/.test(u)) return '正常';
    return s;
  }

  function cellStat(label, value, tone) {
    return '<div class="cell-stat' + (tone ? ' ' + tone : '') + '"><span>' + esc(label) + '</span><b>' +
      esc(value == null || value === '' ? '—' : value) + '</b></div>';
  }

  function renderCellView() {
    const host = $('#cellBody');
    if (!host) return;
    const c = state.console;
    const m = (c && c.modem) || {};
    const present = !!m.present;
    const connected = !!m.connected;
    const badge = $('#cellBadge');
    if (badge) {
      badge.textContent = !c ? '读取中' : (present ? (connected ? '已连接' : '未连接') : '未检测到模组');
    }
    if (state.cellLoading && !c) {
      host.innerHTML = '<div class="cell-empty">正在读取模组状态…</div>';
      return;
    }
    if (state.cellError) {
      host.innerHTML = '<div class="cell-empty">' + esc(state.cellError) + '</div>';
      return;
    }
    if (!c) {
      host.innerHTML = '<div class="cell-empty">暂无数据</div>';
      return;
    }
    if (!present && !m.installed) {
      host.innerHTML = '<div class="cell-empty">设备没有检测到蜂窝模组。</div>';
      return;
    }

    const noSim = /ABSENT|NOT INSERTED|NO SIM|MISS|ERROR|FAIL/i.test(m.simStatus || '');
    const statusText = !present ? '未检测到模组' : (noSim ? '未插 SIM 卡' : (connected ? '已连接' : '未连接'));
    const mode = m.mode || '—';
    const sub = [m.model, m.interface].filter(Boolean).join(' · ');
    const rsrp = m.rsrp ? m.rsrp + ' dBm' : '—';
    const sinr = m.sinr ? m.sinr + ' dB' : '—';
    const rsrpTone = m.rsrp && m.rsrp <= -110 ? 'bad' : (m.rsrp && m.rsrp <= -95 ? 'warn' : '');
    const simText = simLabel(m.simStatus);
    const operatorText = m.operator || (connected ? '已注册网络' : (noSim ? '未插卡，未注册' : '未注册网络'));
    const uptime = m.uptime ? shortDur(m.uptime) : '—';
    const raw = m.raw || '';

    host.innerHTML =
      '<div class="cell-hero">' +
        '<div class="cell-mode">' + esc(mode) + '</div>' +
        '<span class="status-pill ' + (connected ? 'on' : '') + '">' + esc(statusText) + '</span>' +
        '<div class="cell-operator">' + esc(present ? operatorText : '未检测到模组') + '</div>' +
        (sub ? '<div class="cell-sub mono">' + esc(sub) + '</div>' : '') +
      '</div>' +
      '<div class="cell-grid">' +
        cellStat('制式', mode) +
        cellStat('运营商', operatorText) +
        cellStat('SIM 状态', simText, noSim ? 'bad' : '') +
        cellStat('信号 RSRP', rsrp, rsrpTone) +
        cellStat('信号 SINR', sinr) +
        cellStat('频段', m.band || '—') +
        cellStat('接口', m.interface || '—') +
        cellStat('IP 地址', m.ipv4 || '—') +
        cellStat('在线时长', uptime) +
        cellStat('连接状态', statusText, connected ? '' : (present ? 'warn' : '')) +
      '</div>' +
      (raw ? '<details class="cell-raw"><summary>原始输出</summary><pre>' + esc(raw) + '</pre></details>' : '');
  }

  async function ensureConfigData() {
    if (!state.api || !state.connected) return;
    if (!state.cfgName) return;
    if (state.cfgVals || state.cfgLoading) return;
    state.cfgLoading = true;
    renderLab();
    try {
      state.cfgVals = await state.api.GetConfigValues(state.cfgName);
    } catch (e) {
      toastError(e);
      state.cfgVals = {};
    } finally {
      state.cfgLoading = false;
      renderLab();
    }
  }

  async function loadConfigValues(name) {
    if (!state.api || !name) return;
    state.cfgName = name;
    state.cfgVals = null;
    await ensureConfigData();
  }

  async function fetchLogs() {
    if (!state.api || !state.connected) return;
    const kindEl = $('#logKind');
    const filterEl = $('#logFilter');
    const linesEl = $('#logLines');
    state.logKind = kindEl ? kindEl.value : state.logKind;
    state.logFilter = filterEl ? filterEl.value.trim() : state.logFilter;
    const lines = linesEl ? parseInt(linesEl.value, 10) || 80 : 80;
    state.logLoading = true;
    renderConsole();
    try {
      state.logOut = await state.api.GetLogs(state.logKind, state.logFilter, lines);
    } catch (e) {
      toastError(e);
    } finally {
      state.logLoading = false;
      renderConsole();
    }
  }

  function scrollLab(id) {
    const el = document.getElementById(id);
    if (el) el.scrollIntoView({ behavior: 'auto', block: 'start' });
  }

  function accOpen(id) {
    return state.accOpen.has(id);
  }

  function acc(id, icon, title, body, badge) {
    const open = accOpen(id);
    return '<div class="acc' + (open ? ' open' : '') + '" id="acc-' + id + '">' +
      '<button type="button" class="acc-head" data-acc="' + id + '">' +
        '<svg class="i"><use href="#i-' + icon + '"/></svg>' + esc(title) +
        (badge ? '<span class="acc-badge">' + esc(badge) + '</span>' : '') +
        '<svg class="i chev"><use href="#i-chevron"/></svg>' +
      '</button>' +
      '<div class="acc-body">' + body + '</div>' +
    '</div>';
  }

  function sectionHead(icon, title, sub) {
    return '<div class="section-head"><svg class="i"><use href="#i-' + icon + '"/></svg>' +
      '<span class="section-title">' + esc(title) + '</span>' +
      (sub ? '<span class="section-sub">' + esc(sub) + '</span>' : '') + '</div>';
  }

  function termBlock(out, id) {
    if (!out) return '<pre class="term" id="' + id + '"></pre>';
    const cmd = '<span class="cmd"># ' + esc(out.command || '') + '</span>';
    const body = esc(out.output || '');
    const meta = '<span class="dot">— exit ' + esc(out.exitCode) + ' · ' + esc(fmtDur(out.durationMs)) + '</span>';
    return '<pre class="term" id="' + id + '">' + cmd + (body ? '\n' + body : '') + '\n' + meta + '</pre>';
  }

  function renderLab() {
    const host = $('#labContent');
    if (!host) return;
    const info = (state.lab && state.lab.info) || state.info || {};
    const c = state.console;
    let crumb;
    if (state.labTab === 'console') crumb = [info.hostname, c && (c.board && c.board.model || c.cpuModel)].filter(Boolean).join(' · ');
    else if (state.labTab === 'market') crumb = '全量插件 → 预检 → dry-run 预演';
    else crumb = [info.hostname, info.model].filter(Boolean).join(' · ');
    $('#labCrumb').textContent = crumb || 'advanced';
    $$('.lab-tabs button').forEach((b) => {
      if (b.dataset.labtab) b.classList.toggle('active', b.dataset.labtab === state.labTab);
    });
    if (state.labTab === 'tools') renderTools();
    else if (state.labTab === 'market') renderMarket();
    else renderConsole();
  }

  /* ================= console ================= */
  const CONSOLE_TABS = [
    ['overview', '概览'], ['system', '系统'], ['network', '接口'], ['wireless', '无线'],
    ['plugins', '插件'], ['packages', '软件包'], ['dhcp', 'DHCP'], ['firewall', '防火墙'],
    ['config', '配置'], ['logs', '日志'], ['process', '进程'], ['storage', '存储'],
  ];

  function setConsoleTab(key) {
    if (state.cTab === key) return;
    state.cTab = key;
    const scroll = $('#labScroll');
    if (scroll) scroll.scrollTop = 0;
    renderConsole();
    if (key === 'logs' && !state.logOut && !state.logLoading) fetchLogs();
    if (key === 'config') ensureConfigData();
    if (key === 'plugins' && !state.console) fetchConsole();
    if (key === 'storage') fetchStorage();
  }

  function pctBar(pct, tone) {
    const p = Math.max(0, Math.min(100, Number(pct) || 0));
    return '<div class="bar"><i class="' + (tone || '') + '" style="width:' + p + '%"></i></div>';
  }

  function renderConsole() {
    const host = $('#labContent');
    if (!host) return;
    const scroll = $('#labScroll');
    const top = scroll ? scroll.scrollTop : 0;
    const c = state.console;
    const parts = ['<div class="ctabs-wrap">' +
      '<button type="button" class="ctabs-nav" data-act="ctabs-left" title="向前" aria-label="向前滚动标签">' +
        '<svg class="i"><use href="#i-back"/></svg></button>' +
      '<div class="ctabs">' + CONSOLE_TABS.map(([k, label]) =>
        '<button type="button" class="chip' + (state.cTab === k ? ' active' : '') + '" data-ctab="' + k + '">' + label + '</button>'
      ).join('') + '</div>' +
      '<button type="button" class="ctabs-nav flip" data-act="ctabs-right" title="向后" aria-label="向后滚动标签">' +
        '<svg class="i"><use href="#i-back"/></svg></button>' +
      '</div>'];

    if (!c) {
      parts.push(state.consoleLoading
        ? '<div class="empty">正在读取设备控制台…</div>'
        : '<div class="empty">' + esc(state.consoleError || '控制台数据不可用，请点击右上角刷新。') + '</div>');
    } else {
      parts.push(renderConsoleBody(c));
    }
    host.innerHTML = parts.join('');
    if (scroll) scroll.scrollTop = top;
    syncCtabs();
  }

  // 子标签滚动区：显示可见滚动条，并在左右还能滚动时提示 + 自动定位当前标签。
  function syncCtabs() {
    const wrap = $('#labContent .ctabs-wrap');
    const tabs = wrap ? wrap.querySelector('.ctabs') : null;
    if (!wrap || !tabs) return;
    const left = wrap.querySelector('[data-act="ctabs-left"]');
    const right = wrap.querySelector('[data-act="ctabs-right"]');
    const update = () => {
      const max = tabs.scrollWidth - tabs.clientWidth;
      const canLeft = tabs.scrollLeft > 2;
      const canRight = tabs.scrollLeft < max - 2;
      wrap.classList.toggle('can-left', canLeft);
      wrap.classList.toggle('can-right', canRight);
      if (left) left.disabled = !canLeft;
      if (right) right.disabled = !canRight;
    };
    if (!tabs._ctBound) {
      tabs.addEventListener('scroll', update, { passive: true });
      tabs._ctBound = true;
    }
    update();
    const active = tabs.querySelector('.chip.active');
    if (active) {
      const l = active.offsetLeft;
      const r = l + active.offsetWidth;
      if (l < tabs.scrollLeft || r > tabs.scrollLeft + tabs.clientWidth) {
        tabs.scrollTo({ left: Math.max(0, l - (tabs.clientWidth - active.offsetWidth) / 2), behavior: 'smooth' });
      }
    }
  }

  function renderConsoleBody(c) {
    switch (state.cTab) {
      case 'system': return consoleSystem(c);
      case 'network': return consoleNetwork(c);
      case 'wireless': return consoleWireless(c);
      case 'plugins': return consolePlugins(c);
      case 'packages': return consolePackages(c);
      case 'dhcp': return consoleDHCP(c);
      case 'firewall': return consoleFirewall(c);
      case 'config': return consoleConfig(c);
      case 'logs': return consoleLogs(c);
      case 'process': return consoleProcesses(c);
      case 'storage': return consoleStorage(c);
      default: return consoleOverview(c);
    }
  }

  /* ================= install (M1 dry-run) ================= */
  const INSTALL_PHASES = {
    prepare: '准备', env: '读取设备环境', index: '拉取索引', resolve: '依赖求解',
    preflight: '预检', download: '下载', verifyLocal: '本地校验', upload: '上传',
    simulate: '离线 dry-run', done: '完成', failed: '失败',
  };

  function instBytes(n) {
    n = Number(n) || 0;
    if (n >= 1048576) return (n / 1048576).toFixed(1) + ' MB';
    if (n >= 1024) return Math.round(n / 1024) + ' KB';
    return n + ' B';
  }

  function instChip(label, value, tone) {
    return '<span class="inst-chip ' + esc(tone || '') + '"><em>' + esc(label) + '</em><b>' + esc(value || '—') + '</b></span>';
  }

  function installJobHtml(job) {
    const phase = INSTALL_PHASES[job.phase] || job.phase || '—';
    let h = '<div class="inst-job">';
    h += '<div class="inst-phase"><span>' + esc(phase) + '</span><b>' + (Number(job.percent) || 0) + '%</b></div>';
    h += pctBar(job.percent || 0, job.result && job.result.success ? 'ok' : '');

    if (job.result) {
      const ok = !!job.result.success;
      const title = ok ? (job.dryRun ? '预检通过' : '安装完成') : (job.dryRun ? '预检未通过' : '安装未完成');
      const human = ok
        ? (job.dryRun ? '依赖、空间和包完整性检查都通过，可以安装。' : '插件已安装，服务状态已回读。')
        : humanJobError(job);
      h += '<div class="inst-result ' + (ok ? 'ok' : 'bad') + '"><b>' + esc(title) + '</b><span>' + esc(human) + '</span></div>';
      h += '<details class="op-detail"><summary>安装详情（依赖、预检、回读、日志）</summary>';
      if (job.result.installed && job.result.installed.length) {
        h += '<div class="note">已装：' + job.result.installed.map(esc).join('、') + '</div>';
      }
      if (job.result.failed) {
        h += '<div class="note bad">失败包：' + esc(job.result.failed) +
          (job.result.pending && job.result.pending.length ? '；未装：' + job.result.pending.map(esc).join('、') : '') + '</div>';
      }
      if (job.result.changes) {
        const ch = job.result.changes;
        const bits = [];
        if (ch.packagesAdded && ch.packagesAdded.length) bits.push('包列表新增 ' + ch.packagesAdded.length + '：' + ch.packagesAdded.map(esc).join('、'));
        if (ch.initAdded && ch.initAdded.length) bits.push('init.d 新增：' + ch.initAdded.map(esc).join('、'));
        if (ch.configChanged && ch.configChanged.length) bits.push('配置变化：' + ch.configChanged.map(esc).join('、'));
        if (bits.length) h += '<div class="note">装后回读：' + bits.join('；') + '</div>';
      }
      if (job.result.simulate) h += '<pre class="term">' + esc(job.result.simulate) + '</pre>';
    }

    if (job.files && job.files.length) {
      h += '<div class="inst-title">文件进度</div>';
      h += job.files.map((f) => {
        const pct = f.size > 0 ? Math.min(100, Math.round((f.sent || 0) * 100 / f.size)) : (f.status === 'ok' || f.status === 'uploaded' ? 100 : 0);
        return '<div class="inst-file"><div><span>' + esc(f.name) + '</span><em>' + esc(f.status) + ' · ' + instBytes(f.size) + '</em></div>' +
          '<div class="bar"><i class="' + (f.status === 'failed' ? 'bad' : '') + '" style="width:' + pct + '%"></i></div></div>';
      }).join('');
    }

    if (job.checks && job.checks.length) {
      h += '<div class="inst-title">预检结论</div>';
      h += job.checks.map((c) =>
        '<div class="inst-check ' + esc(c.level || '') + '"><span class="tag">' + esc(c.stage || '') + '</span>' +
        '<b>' + esc(c.label || c.id) + '</b><em>' + esc(c.detail || '') + '</em></div>').join('');
    }

    if (job.plan && job.plan.packages && job.plan.packages.length) {
      const p = job.plan;
      h += '<div class="inst-title">依赖计划 · ' + p.packages.length + ' 个包 · ' + instBytes(p.totalSize) + '</div>';
      if (p.warnings && p.warnings.length) {
        h += '<div class="note warn">' + p.warnings.map((w) => esc(w)).join('<br>') + '</div>';
      }
      h += '<div class="inst-plan">' + p.packages.map((it) => {
        const dep = it.isRoot ? '目标' : (it.reason || '依赖');
        return '<div class="inst-plan-row"><div><b>' + esc(it.name) + '</b> <em>' + esc(it.version) + '</em>' +
          '<span class="inst-arch">' + esc(it.arch || '') + '</span></div>' +
          '<div class="inst-reason">' + esc(dep) + ' · ' + esc(it.status || '') + ' · ' + instBytes(it.size) + '</div></div>';
      }).join('') + '</div>';
    }

    if (job.log && job.log.length) {
      const lines = job.log.slice(-120).join('\n');
      h += '<div class="inst-title">执行日志</div><pre class="term">' + esc(lines) + '</pre>';
    }
    if (job.result) h += '</details>';
    h += '</div>';
    return h;
  }

  function humanJobError(job) {
    const r = job.result || {};
    const code = String(r.code || '');
    if (/overlay|space|disk/.test(code)) return '设备可写空间不足，先清理空间再安装。';
    if (/depend|missing/.test(code)) return '缺少依赖，刷新索引或换一个版本再试。';
    if (/arch/.test(code)) return '包架构和设备不匹配。';
    if (/download|network|timeout/.test(code)) return '下载失败，检查设备和包源的网络。';
    if (/verify|checksum|signature/.test(code)) return '包完整性校验没通过，已停止安装。';
    const msg = String(r.message || '').trim();
    return msg && msg.length <= 80 ? msg : '操作没有完成，展开详情查看具体原因。';
  }

  /* ================= 插件市场 ================= */
  const MARKET_REPOS_KEY = 'prolite-market-repos';
  const MARKET_ZH = (typeof window !== 'undefined' && window.MARKET_ZH) || {};

  function marketSavedRepos() {
    try {
      return String(localStorage.getItem(MARKET_REPOS_KEY) || '')
        .split('\n').map((s) => s.trim()).filter((s) => s && s[0] !== '#');
    } catch (e) { return []; }
  }

  function marketRepoLines() {
    return String(state.market.reposText || '')
      .split('\n').map((s) => s.trim()).filter((s) => s && s[0] !== '#');
  }

  function marketZhFor(name) {
    const n = String(name || '');
    return MARKET_ZH[n] || MARKET_ZH[n.replace(/^luci-app-/, '')] || MARKET_ZH['luci-app-' + n] || null;
  }

  async function ensureMarketRepos(force) {
    const mk = state.market;
    if (mk.reposTouched && !force) return;
    if (String(mk.reposText || '').trim() && !force) { mk.reposTouched = true; return; }
    const saved = marketSavedRepos();
    if (saved.length) {
      mk.reposText = saved.join('\n');
      mk.reposTouched = true;
      renderMarket();
      return;
    }
    if (!state.connected) { renderMarket(); return; }
    if (!state.api || !state.api.GetMarketRepos) return;
    try {
      const env = await state.api.GetMarketRepos();
      mk.env = env;
      mk.envErr = (env && env.note) || '';
      if (env && env.feeds && env.feeds.length) {
        mk.reposText = env.feeds.map((f) => f.url).join('\n');
        mk.reposTouched = true;
      }
    } catch (e) {
      mk.envErr = errText(e);
    }
    if (state.labTab === 'market') renderMarket();
  }

  function marketCatOf(p) {
    const sec = String(p.section || '').trim();
    if (sec) return sec;
    const n = String(p.name || '').toLowerCase();
    if (n.indexOf('luci-app-') === 0) return 'LuCI 应用';
    if (n.indexOf('luci-i18n-') === 0) return 'LuCI 语言包';
    if (n.indexOf('luci-') === 0) return 'LuCI 组件';
    if (n.indexOf('kmod-') === 0) return '内核模块';
    if (n.indexOf('lib') === 0) return '库';
    if (n.indexOf('python3-') === 0) return 'Python';
    if (n.indexOf('perl-') === 0) return 'Perl';
    if (n.indexOf('php') === 0) return 'PHP';
    if (n.indexOf('node-') === 0) return 'Node';
    if (n.indexOf('golang-') === 0) return 'Go';
    if (n.indexOf('qmodem') === 0) return '蜂窝模组';
    return String(p.origin || '其他').trim() || '其他';
  }

  function marketZh(p) {
    if (p && p.zh) return p.zh;
    const m = marketZhFor(p && p.name);
    return m ? m.d : '';
  }

  function pkgRisk(name) {
    const n = String(name || '').toLowerCase();
    if (n.indexOf('kmod-') === 0) return '内核模块';
    if (n === 'kernel' || n.indexOf('linux-') === 0) return '内核本体';
    if (n.indexOf('uboot') === 0 || n.indexOf('bootloader') >= 0) return 'Bootloader';
    if (n.indexOf('base-files') === 0) return '基础系统';
    if (n === 'libc' || n.indexOf('libc-') === 0) return '系统库';
    if (n === 'apk' || n.indexOf('opkg') === 0) return '包管理器';
    if (n.indexOf('mtd') === 0 || n.indexOf('firmware') >= 0) return '固件';
    return '';
  }

  function marketCats() {
    const set = new Set();
    ((state.market.index && state.market.index.packages) || []).forEach((p) => set.add(marketCatOf(p)));
    return Array.from(set).sort();
  }

  function marketFiltered() {
    const mk = state.market;
    const q = String(mk.search || '').trim().toLowerCase();
    let list = ((mk.index && mk.index.packages) || []).slice();
    if (mk.onlyNew) list = list.filter((p) => !p.installed);
    if (mk.cat) list = list.filter((p) => marketCatOf(p) === mk.cat);
    if (q) {
      list = list.filter((p) => {
        const zh = marketZh(p);
        return p.name.toLowerCase().indexOf(q) >= 0 ||
          String(p.description || '').toLowerCase().indexOf(q) >= 0 ||
          (zh && zh.toLowerCase().indexOf(q) >= 0);
      });
    }
    if (mk.sort === 'size') list.sort((a, b) => (b.size || 0) - (a.size || 0));
    else if (mk.sort === 'name') list.sort((a, b) => a.name.localeCompare(b.name));
    else list.sort((a, b) => compareNameVer(a, b));
    return list;
  }

  function compareNameVer(a, b) {
    if (a.name === b.name) return String(b.version || '').localeCompare(String(a.version || ''));
    return a.name.localeCompare(b.name);
  }

  function marketListHtml() {
    const mk = state.market;
    const list = marketFiltered();
    const pages = Math.max(1, Math.ceil(list.length / mk.pageSize));
    const page = Math.max(0, Math.min(mk.page, pages - 1));
    const start = page * mk.pageSize;
    const rows = list.slice(start, start + mk.pageSize).map((p) => {
      const zh = marketZh(p);
      const risk = pkgRisk(p.name);
      const desc = zh || p.description || '索引暂未提供说明';
      const untrans = zh ? '' : '<span class="mk-untrans">待翻译</span>';
      const sel = mk.selected && mk.selected.name === p.name && mk.selected.version === p.version ? ' selected' : '';
      return '<button type="button" class="mk-card' + sel + '" data-act="market-select" data-name="' + esc(p.name) + '" data-ver="' + esc(p.version) + '">' +
        '<span class="mk-card-head"><b class="mono">' + esc(p.name) + '</b>' +
          (risk ? '<span class="mk-risk">' + esc(risk) + '</span>' : '') +
          (p.installed ? '<span class="mk-badge on">已安装</span>' : '<span class="mk-badge">未安装</span>') + '</span>' +
        '<span class="mk-card-desc">' + esc(clipText(desc, 90)) + ' ' + untrans + '</span>' +
        (risk ? '<span class="mk-risk-note">安装后可能导致设备无法启动</span>' : '') +
        '<span class="mk-card-foot"><span class="mono">' + esc(p.version) + '</span>' +
          '<span class="mono">' + fmtBytes(p.size) + '</span>' +
          '<span class="tag">' + esc(marketCatOf(p)) + '</span></span>' +
        '</button>';
    }).join('');
    const pager = '<div class="mk-pager"><span class="mono">共 ' + list.length + ' 个 · 第 ' + (page + 1) + '/' + pages + ' 页</span>' +
      '<span class="grow"></span>' +
      '<button type="button" class="btn mini ghost" data-act="market-page" data-page="0"' + (page <= 0 ? ' disabled' : '') + '>首页</button>' +
      '<button type="button" class="btn mini ghost" data-act="market-page" data-page="' + (page - 1) + '"' + (page <= 0 ? ' disabled' : '') + '>上一页</button>' +
      '<button type="button" class="btn mini ghost" data-act="market-page" data-page="' + (page + 1) + '"' + (page >= pages - 1 ? ' disabled' : '') + '>下一页</button>' +
      '<button type="button" class="btn mini ghost" data-act="market-page" data-page="' + (pages - 1) + '"' + (page >= pages - 1 ? ' disabled' : '') + '>末页</button>' +
      '</div>';
    return '<div class="mk-cards">' + (rows || '<div class="empty">没有匹配的包，试试清除搜索或取消“只看未安装”。</div>') + '</div>' + pager;
  }

  function updateMarketList() {
    const wrap = $('#mkListWrap');
    if (wrap) wrap.innerHTML = marketListHtml();
  }

  function renderMarket() {
    if (state.labTab !== 'market') return;
    const host = $('#labContent');
    if (!host) return;
    const mk = state.market;
    const scroll = $('#labScroll');
    const top = scroll ? scroll.scrollTop : 0;

    let h = sectionHead('plug', '插件市场', '设备源全部插件 · 每页 10 个');
    if (!state.connected) {
      h += '<div class="note bad">未连接设备：无法读取默认仓库，也无法做依赖预检；可手动填写仓库并浏览索引。</div>';
    }
    if (mk.env && mk.env.manager) {
      h += '<div class="inst-env">' +
        instChip('包管理器', mk.env.manager, 'ok') +
        instChip('架构', mk.env.arch, mk.env.arch ? 'ok' : 'bad') +
        instChip('内核', mk.env.kernel, 'ok') +
        instChip('已装包', String(mk.env.installed || 0), 'ok') +
        '</div>';
    }
    if (mk.envErr) h += '<div class="note">' + esc(mk.envErr) + '</div>';

    if (mk.indexErr) h += '<div class="note bad">' + esc(mk.indexErr) + '</div>';
    if (mk.loading && !mk.index) {
      h += '<div class="empty">正在从设备源读取插件索引…</div>';
    }

    if (mk.index) {
      const installed = (mk.index.packages || []).filter((p) => p.installed).length;
      h += '<div class="inst-env">' +
        instChip('全部插件', String(mk.index.total || 0), 'ok') +
        instChip('已安装', String(installed), 'ok') +
        instChip('仓库数', String((mk.index.sources || []).length), 'ok') +
        instChip('用时', String(mk.index.elapsedMs || 0) + ' ms', 'ok') +
        '</div>';
      h += '<div class="mk-filters">' +
        '<input id="mkSearch" class="input" placeholder="搜索包名 / 中文说明" spellcheck="false" value="' + esc(mk.search) + '">' +
        '<select id="mkCat" class="input mk-sort">' +
          '<option value="">全部类别</option>' +
          marketCats().map((c) => '<option value="' + esc(c) + '"' + (mk.cat === c ? ' selected' : '') + '>' + esc(c) + '</option>').join('') +
        '</select>' +
        '<label class="mk-check"><input type="checkbox" id="mkOnlyNew"' + (mk.onlyNew ? ' checked' : '') + '> 只看未安装</label>' +
        '<select id="mkSort" class="input mk-sort">' +
          '<option value="name"' + (mk.sort === 'name' ? ' selected' : '') + '>按名称</option>' +
          '<option value="size"' + (mk.sort === 'size' ? ' selected' : '') + '>按体积（大到小）</option>' +
        '</select>' +
        '<button type="button" class="btn mini ghost" data-act="market-fetch"' + (mk.loading ? ' disabled' : '') + '>' + (mk.loading ? '刷新中…' : '刷新') + '</button>' +
        '</div>';
      h += '<div id="mkListWrap">' + marketListHtml() + '</div>';
      h += marketDetailHtml();
      h += marketPlanHtml();
    } else if (!mk.loading) {
      h += '<div class="empty">还没有读取到插件索引。可在下方“高级”里检查设备源或手动填写仓库。</div>';
    }

    h += '<details class="mk-advanced" data-mkdetail="repo"' + (mk.repoOpen ? ' open' : '') + '><summary>高级：自定义仓库 / 索引诊断</summary><div class="mk-advanced-body">';
    h += '<div class="mk-repo-box">' +
      '<div class="mk-label">仓库地址（每行一个；支持 apk 的 APKINDEX.tar.gz 与 opkg 的 Packages.gz）</div>' +
      '<textarea id="mkRepos" class="mk-repos" spellcheck="false" placeholder="https://downloads.immortalwrt.org/releases/.../packages/x86_64/luci">' + esc(mk.reposText) + '</textarea>' +
      '<div class="row-actions">' +
        '<button type="button" class="btn mini ghost" data-act="market-device-repos">读取设备源</button>' +
        '<button type="button" class="btn mini ghost" data-act="market-save-repos">保存地址</button>' +
        '<button type="button" class="btn mini primary" data-act="market-fetch"' + (mk.loading ? ' disabled' : '') + '>' + (mk.loading ? '正在拉取索引…' : '测试连通性 / 拉取索引') + '</button>' +
      '</div>' +
    '</div>';
    if (mk.checks && mk.checks.length) {
      h += '<div class="mk-checks">' + mk.checks.map((c) =>
        '<div class="inst-check ' + esc(c.level || '') + '"><span class="tag">' + esc(c.stage || '') + '</span>' +
        '<b>' + esc(c.label || c.id) + '</b><em>' + esc(c.detail || '') + '</em></div>').join('') + '</div>';
    }
    h += '</div></details>';

    if (mk.job) h += installJobHtml(mk.job);
    host.innerHTML = h;
    if (scroll) scroll.scrollTop = top;
  }

  function marketDetailHtml() {
    const mk = state.market;
    const p = mk.selected;
    if (!p) return '';
    const zh = marketZh(p);
    const risk = pkgRisk(p.name);
    const deps = (p.depends || []).length
      ? '<div class="mk-deps">' + p.depends.map((d) => '<span class="mk-dep mono">' + esc(d) + '</span>').join('') + '</div>'
      : '<div class="note">索引未声明依赖</div>';
    let h = '<div class="mk-detail">' + sectionHead('plug', '包详情', p.name + ' ' + p.version);
    h += '<div class="kv">' +
      '<span><b>版本</b> ' + esc(p.version) + '</span>' +
      '<span><b>架构</b> ' + esc(p.arch || '—') + '</span>' +
      '<span><b>下载体积</b> ' + fmtBytes(p.size) + '</span>' +
      '<span><b>安装后体积</b> ' + fmtBytes(p.installedSize) + '</span>' +
      '<span><b>许可证</b> ' + esc(p.license || '索引未提供') + '</span>' +
      '<span><b>分类</b> ' + esc(p.section || p.origin || '索引未提供') + '</span>' +
      '<span><b>状态</b> ' + (p.installed ? '已安装' + (p.installedVersion ? ' ' + esc(p.installedVersion) : '') : '未安装') + '</span>' +
      '</div>';
    h += '<div class="mk-origin">来源仓库：<span class="mono">' + esc(p.feed || '—') + '</span>' +
      (p.homepage ? ' · <span class="mono">' + esc(p.homepage) + '</span>' : '') + '</div>';
    h += '<div class="mk-zh">' + (zh ? esc(zh) : esc(p.description || '索引暂未提供说明') + ' <span class="mk-untrans">待翻译</span>') + '</div>';
    if (risk) h += '<div class="mk-risk-line">' + esc(risk) + '：安装后可能导致设备无法启动，需要额外二次确认。</div>';
    h += '<div class="mk-label">依赖' + ((p.depends || []).length ? '（' + p.depends.length + '）' : '') + '</div>' + deps;
    h += '<div class="row-actions">' +
      '<button type="button" class="btn mini primary" data-act="market-plan"' + (mk.planLoading ? ' disabled' : '') + '>' + (mk.planLoading ? '预检中…' : '加入计划 / 预检') + '</button>' +
      '</div>';
    if (mk.planErr) h += '<div class="note bad">' + esc(mk.planErr) + '</div>';
    h += '</div>';
    return h;
  }

  function marketPlanHtml() {
    const mk = state.market;
    const view = mk.plan;
    if (!view || !view.plan) return '';
    const plan = view.plan;
    const checks = view.checks || [];
    const fails = checks.filter((c) => c.level === 'fail');
    const warns = checks.filter((c) => c.level === 'warn');
    const pkgs = plan.packages || [];
    const danger = pkgs.filter((p) => p.dangerous);
    let h = '<div class="mk-plan">' + sectionHead('check', '预检与安装计划',
      pkgs.length + ' 个包 · 下载 ' + instBytes(plan.totalSize) + ' · 安装后 ' + instBytes(plan.totalInstall));
    h += '<div class="inst-env">' +
      instChip('运行结果', fails.length ? '不可安装' : (warns.length ? '可安装（有提示）' : '可安装'), fails.length ? 'bad' : (warns.length ? 'warn' : 'ok')) +
      instChip('系统组件', danger.length ? String(danger.length) + ' 个' : '无', danger.length ? 'warn' : 'ok') +
      '</div>';
    if (checks.length) {
      h += '<div class="inst-title">预检结论</div>';
      h += checks.map((c) =>
        '<div class="inst-check ' + esc(c.level || '') + '"><span class="tag">' + esc(c.stage || '') + '</span>' +
        '<b>' + esc(c.label || c.id) + '</b><em>' + esc(c.detail || '') + '</em></div>').join('');
    }
    if (plan.warnings && plan.warnings.length) {
      h += '<div class="note warn">' + plan.warnings.map((w) => esc(w)).join('<br>') + '</div>';
    }
    h += '<div class="inst-title">依赖树（先依赖，后被依赖）</div><div class="inst-plan">' +
      pkgs.map((it) => '<div class="inst-plan-row"><div><b>' + esc(it.name) + '</b> <em>' + esc(it.version) + '</em>' +
        '<span class="inst-arch">' + esc(it.arch || '') + '</span>' +
        (it.dangerous ? '<span class="mk-risk">' + esc(pkgRisk(it.name) || '系统组件') + '</span>' : '') + '</div>' +
        '<div class="inst-reason">' + esc(it.isRoot ? '目标包' : (it.reason || '依赖')) + ' · ' + fmtBytes(it.size) + '</div></div>').join('') +
      '</div>';
    h += '<label class="mk-check mk-confirm"><input type="checkbox" id="mkConfirm"' + (mk.confirmed ? ' checked' : '') + '> 我已确认包名、来源仓库与安装顺序</label>';
    if (danger.length) {
      h += '<div class="note bad">计划包含 ' + danger.length + ' 个系统组件（kmod / 内核 / 引导 / base-files / libc / apk / opkg），安装后可能导致设备无法启动。</div>';
    }
    if (fails.length) {
      h += '<div class="note bad">存在 fail 项（如 overlay 空间不足），暂不能安装。</div>';
    }
    h += '<div class="row-actions">' +
      '<button type="button" class="btn" data-act="market-dryrun"' + (mk.starting ? ' disabled' : '') + '>先做 dry-run 预演（不写入）</button>' +
      '</div>';
    h += marketInstallBox(pkgs, danger);
    h += '<div class="note">下载与上传均做 SHA256 校验；安装前会自动做设备包与 init.d 快照。</div>';
    h += '</div>';
    return h;
  }

  function policyReady() {
    const pol = state.market.policy;
    return !!(pol && pol.configured && pol.matches && pol.allowOverlay && pol.allowTmp);
  }

  function marketInstallReady() {
    const mk = state.market;
    const host = (state.info && state.info.host) || '';
    const view = mk.plan;
    const pkgs = (view && view.plan && view.plan.packages) || [];
    const danger = pkgs.some((p) => p.dangerous && !p.installed);
    const fails = ((view && view.checks) || []).some((c) => c.level === 'fail');
    const polDangerOK = !danger || !!(mk.policy && mk.policy.allowDangerous);
    return !!(mk.selected && view && view.plan && !fails && policyReady() && polDangerOK && mk.confirmed &&
      mk.confirmHost === host && mk.confirmA && mk.confirmB && (!danger || mk.allowDangerous));
  }

  function marketInstallBox(pkgs, danger) {
    const mk = state.market;
    const host = (state.info && state.info.host) || '';
    const pol = mk.policy;
    const pending = pkgs.filter((p) => !p.installed).length;
    let h = '<div class="mk-confirm-box">' + sectionHead('check', '安装到设备', '确认后才会下载与写入');
    h += '<div class="note warn">将向 <b class="mono">' + esc(host || '当前设备') + '</b> 写入 ' + pending +
      ' 个包；安装前会做快照，失败时不会自动卸载已完成的包。</div>';
    if (!policyReady()) {
      h += '<div class="note bad">设备写入授权未开启：' + esc((pol && pol.error) || mk.policyErr || '未配置 device-write-allow.json') + '</div>';
      h += '<div class="note">授权文件：<span class="mono">' + esc((pol && pol.path) || '') +
        '</span><br>示例：<span class="mono">{"host":"' + esc(host || '设备地址') + '","allowOverlay":true,"allowTmp":true,"allowDangerous":' +
        (danger.length ? 'true' : 'false') + '}</span></div>';
    } else if (danger.length && !pol.allowDangerous) {
      h += '<div class="note bad">授权文件未允许系统组件写入：请把 <span class="mono">"allowDangerous"</span> 设为 <span class="mono">true</span>。</div>';
    }
    h += '<label class="mk-label">逐字输入设备地址确认</label>';
    h += '<input id="mkConfirmHost" class="input" spellcheck="false" placeholder="' + esc(host) + '" value="' + esc(mk.confirmHost) + '">';
    h += '<label class="mk-check"><input type="checkbox" id="mkConfirmA"' + (mk.confirmA ? ' checked' : '') + '> 我确认写入设备</label>';
    h += '<label class="mk-check"><input type="checkbox" id="mkConfirmB"' + (mk.confirmB ? ' checked' : '') + '> 我确认写入 overlay 与 /tmp</label>';
    if (danger.length) {
      h += '<label class="mk-check mk-confirm danger"><input type="checkbox" id="mkDangerInstall"' + (mk.allowDangerous ? ' checked' : '') +
        '> 计划含系统组件，可能导致设备无法启动，我仍要继续</label>';
    }
    h += '<div class="row-actions"><button type="button" class="btn primary" data-act="market-install"' +
      (marketInstallReady() ? '' : ' disabled') + '>' + (mk.starting ? '正在启动…' : '开始安装到设备') + '</button></div>';
    h += '</div>';
    return h;
  }

  function syncMarketInstall() {
    const btn = document.querySelector('[data-act="market-install"]');
    if (btn) btn.disabled = !marketInstallReady();
  }

  async function loadMarketPolicy() {
    const mk = state.market;
    if (!state.connected || !state.api || !state.api.GetDeviceWritePolicy) return;
    try {
      mk.policy = await state.api.GetDeviceWritePolicy();
      mk.policyErr = '';
    } catch (e) {
      mk.policyErr = errText(e);
    }
    renderMarket();
  }

  async function installMarketSelected() {
    const mk = state.market;
    if (!marketInstallReady()) { toast('请先完成预检、设备地址与写入确认', 'error'); return; }
    if (mk.starting) return;
    const danger = ((mk.plan.plan && mk.plan.plan.packages) || []).some((p) => p.dangerous && !p.installed);
    mk.starting = true;
    mk.job = null;
    mk.jobId = '';
    renderMarket();
    try {
      const id = await state.api.StartInstallJob({
        mode: 'feed',
        package: mk.selected.name,
        version: mk.selected.version,
        localPath: '',
        dryRun: false,
        repos: marketRepoLines(),
        commit: true,
        confirmed: true,
        allowDangerous: !!danger && mk.allowDangerous,
        confirmWrites: true,
        confirmText: mk.confirmHost,
      });
      mk.jobId = id;
      pollMarketJob(id);
    } catch (e) {
      mk.starting = false;
      toastError(e);
      renderMarket();
    }
  }

  function selectMarketPkg(name, version) {
    const mk = state.market;
    const list = (mk.index && mk.index.packages) || [];
    mk.selected = list.find((p) => p.name === name && p.version === version) || null;
    mk.plan = null;
    mk.planErr = '';
    mk.confirmed = false;
    mk.allowDangerous = false;
    mk.confirmHost = '';
    mk.confirmA = false;
    mk.confirmB = false;
    renderMarket();
  }

  function ensureMarketIndex() {
    const mk = state.market;
    if (!state.connected || mk.index || mk.loading) return;
    if (!state.api || !state.api.GetMarketIndex) return;
    fetchMarketIndex();
  }

  async function fetchMarketIndex() {
    const mk = state.market;
    const repos = marketRepoLines();
    if (!repos.length && !state.connected) { toast('请填写至少一个仓库地址', 'error'); return; }
    if (!state.connected && !state.api) { toast('运行时不可用', 'error'); return; }
    if (!state.api || !state.api.GetMarketIndex) { toast('运行时缺少 GetMarketIndex，请使用最新 exe', 'error'); return; }
    mk.loading = true;
    mk.indexErr = '';
    renderMarket();
    try {
      const view = await state.api.GetMarketIndex(repos);
      mk.index = view;
      mk.checks = (view && view.checks) || [];
      mk.page = 0;
      mk.selected = null;
      mk.plan = null;
      mk.confirmed = false;
      mk.allowDangerous = false;
      mk.confirmHost = '';
      mk.confirmA = false;
      mk.confirmB = false;
      if (view && !view.total) mk.indexErr = '所有仓库都没有解析到包，请检查地址是否指向 feed 根目录（失败详情见上方）。';
    } catch (e) {
      mk.indexErr = errText(e);
    } finally {
      mk.loading = false;
      renderMarket();
    }
  }

  async function resolveMarketPlan() {
    const mk = state.market;
    if (!state.connected) { toast('未连接设备：无法做依赖预检', 'info'); return; }
    if (!mk.selected) { toast('请先选择一个包', 'info'); return; }
    if (!state.api || !state.api.ResolvePlan) { toast('运行时缺少 ResolvePlan，请使用最新 exe', 'error'); return; }
    mk.planLoading = true;
    mk.planErr = '';
    mk.plan = null;
    renderMarket();
    try {
      mk.plan = await state.api.ResolvePlan({
        mode: 'feed',
        package: mk.selected.name,
        version: mk.selected.version,
        repos: marketRepoLines(),
      });
    } catch (e) {
      mk.planErr = errText(e);
    } finally {
      mk.planLoading = false;
      renderMarket();
    }
  }

  async function startMarketJob() {
    const mk = state.market;
    if (!state.connected) { toast('未连接设备：安装预检不可用', 'info'); return; }
    if (!mk.selected) { toast('请先选择一个包', 'info'); return; }
    if (!mk.plan || !mk.plan.plan) { toast('请先点击“加入计划 / 预检”', 'info'); return; }
    if ((mk.plan.checks || []).some((c) => c.level === 'fail')) {
      toast('预检存在 fail 项，禁止进入安装链路', 'error');
      return;
    }
    if (mk.starting) return;
    mk.starting = true;
    mk.job = null;
    mk.jobId = '';
    renderMarket();
    try {
      const id = await state.api.StartInstallJob({
        mode: 'feed',
        package: mk.selected.name,
        version: mk.selected.version,
        localPath: '',
        dryRun: true,
        repos: marketRepoLines(),
        commit: false,
        confirmed: false,
        allowDangerous: false,
      });
      mk.jobId = id;
      pollMarketJob(id);
    } catch (e) {
      mk.starting = false;
      toastError(e);
      renderMarket();
    }
  }

  async function pollMarketJob(id) {
    const mk = state.market;
    clearTimeout(mk.pollTimer);
    try {
      const job = await state.api.GetInstallJob(id);
      mk.job = job;
      mk.starting = false;
      if (job && !job.running && job.result && job.result.success && job.result.code !== 'DRY_RUN_OK') {
        const done = (job.plan && job.plan.packages) || [];
        done.forEach((it) => {
          const q = ((mk.index && mk.index.packages) || []).find((x) => x.name === it.name);
          if (q) { q.installed = true; q.installedVersion = it.version; }
        });
        mk.confirmHost = '';
        mk.confirmA = false;
        mk.confirmB = false;
      }
      renderMarket();
      if (job && job.running) {
        mk.pollTimer = setTimeout(() => pollMarketJob(id), 800);
      }
    } catch (e) {
      mk.starting = false;
      toastError(e);
      renderMarket();
    }
  }


  function consoleOverview(c) {
    const info = state.info || {};
    const mem = c.memory || {};
    const memUsed = (mem.total || 0) - (mem.available || 0);
    const memPct = mem.total ? memUsed / mem.total * 100 : 0;
    const sys = (state.snap && state.snap.system) || {};
    const root = (c.storage || []).find((s) => s.mount === '/') || {};
    const flashTotal = Number(sys.flashTotal) || 0;
    const flashFree = Number(sys.flashFree) || 0;
    const flashLow = flashTotal > 0 && flashFree < 1048576;
    const plugins = c.plugins || [];
    const running = plugins.filter((p) => p.running).length;
    const stat = (label, value, cls) => '<div class="stat"><span class="stat-label">' + label + '</span><span class="stat-value mono ' + (cls || '') + '">' + esc(value || '—') + '</span></div>';
    return '<div class="lab-section device-strip">' +
        '<div class="device-name"><svg class="i" style="color:var(--accent-blue)"><use href="#i-router"/></svg>' +
        esc(c.hostname || info.hostname || 'OpenWrt') +
        '<span class="link-badge mono">' + esc(sys.kernel || c.kernel || 'SSH') + '</span></div>' +
        '<div class="device-meta"><span>' + esc(c.board && c.board.model || info.model || '') + '</span>' +
        (c.cpuModel ? '<span>' + esc(c.cpuModel) + '</span>' : '') +
        (c.arch ? '<span>' + esc(c.arch) + '</span>' : '') + '</div>' +
      '</div>' +
      '<div class="lab-section"><div class="stat-grid">' +
        stat('运行时间', shortDur(c.uptime)) +
        stat('CPU', (c.cpuCores || '—') + ' 核' + (c.cpuFreqMHz ? ' @ ' + c.cpuFreqMHz + 'MHz' : '')) +
        stat('负载', sys.load1 != null ? Number(sys.load1).toFixed(2) : '—') +
        stat('温度', sys.tempAvailable ? Math.round(sys.tempC) + '°C' : '—') +
        stat('内存', mem.total ? Math.round(memUsed / 1048576) + '/' + Math.round(mem.total / 1048576) + ' MB' : '—') +
        stat('根分区', flashTotal ? fmtBytes(flashFree) + ' 空闲' : (root.available ? root.available + ' KB 可用' : '—'), flashLow ? 'crit' : '') +
        stat('软件包', c.packageCount + ' 个') +
        stat('插件', running + '/' + plugins.length + ' 运行') +
      '</div>' +
      (flashLow ? '<div class="space-warn">根分区只剩 ' + esc(fmtBytes(flashFree)) + ' / ' + esc(fmtBytes(flashTotal)) + '，继续写入可能失败。</div>' : '') +
      '<div class="subblock-title" style="margin-top:9px">内存占用</div>' + pctBar(memPct, memPct >= 85 ? 'crit' : memPct >= 70 ? 'warn' : '') +
      '<div class="note" style="margin-top:5px">' + esc(c.date || '') + ' · ' + esc(c.release && (c.release.description || c.release.distribution) || '') + '</div>' +
      '</div>' +
      '<div class="lab-section"><div class="subblock-title">已识别插件</div>' +
        (plugins.length ? '<div class="plugin-chips">' + plugins.map((p) =>
          '<span class="plugin-chip' + (p.running ? ' on' : '') + '" title="' + esc((p.package || p.name) + (p.version ? ' ' + p.version : '')) + '">' + esc(p.name) + '</span>'
        ).join('') + '</div>' : '<div class="empty">未识别到插件</div>') +
      '</div>';
  }

  function consoleSystem(c) {
    const mem = c.memory || {};
    return '<div class="lab-section">' +
      sectionHead('router', '系统身份', c.systemSection || 'system') +
      '<div class="form-grid" style="margin-top:8px">' +
        '<div class="field full"><label>主机名</label><input id="sysHost" value="' + esc(c.hostname) + '" spellcheck="false"></div>' +
        '<div class="field"><label>时区</label><input id="sysTZ" value="' + esc(c.timezone) + '" spellcheck="false" placeholder="CST-8"></div>' +
        '<div class="field"><label>区域名</label><input id="sysZone" value="' + esc(c.zoneName) + '" spellcheck="false" placeholder="Asia/Shanghai"></div>' +
        '<div class="field full"><label>NTP 服务器</label><input id="sysNTP" value="' + esc((c.ntpServers || []).join(', ')) + '" spellcheck="false"></div>' +
      '</div>' +
      '<div class="form-actions"><button type="button" class="btn mini primary" data-act="sys-save">保存并应用</button></div>' +
      '<div class="note">写入 system 配置并重启 sysntpd；主机名立即生效。</div>' +
    '</div>' +
    '<div class="lab-section"><div class="subblock-title">系统信息</div><div class="kv">' +
      '<span><b>发行版</b> ' + esc(c.release && (c.release.description || c.release.distribution) || '—') + '</span>' +
      '<span><b>版本</b> ' + esc(c.release && (c.release.version || c.release.revision) || '—') + '</span>' +
      '<span><b>内核</b> ' + esc(c.kernel) + '</span>' +
      '<span><b>架构</b> ' + esc(c.arch) + '</span>' +
      '<span><b>CPU</b> ' + esc(c.cpuModel || '—') + '</span>' +
      '<span><b>时间</b> ' + esc(c.date) + '</span>' +
    '</div>' +
    (c.uname ? '<div class="raw">' + esc(c.uname) + '</div>' : '') +
    '<div class="subblock-title" style="margin-top:9px">内存</div>' +
    '<div class="kv"><span><b>总计</b> ' + fmtBytes(mem.total) + '</span>' +
      '<span><b>可用</b> ' + fmtBytes(mem.available) + '</span>' +
      '<span><b>缓存</b> ' + fmtBytes(mem.cached) + '</span>' +
      '<span><b>Swap</b> ' + fmtBytes(mem.swapTotal) + '</span></div>' +
    '</div>';
  }

  function consoleNetwork(c) {
    const rows = (c.interfaces || []).map((it) => {
      const up = it.up;
      const ips = (it.ips || []).join(', ') || '无地址';
      const actions = it.cellModem
        ? '<div class="row if-actions"><button type="button" class="btn mini primary" data-act="cell-detail">蜂窝详情</button></div>'
        : '<div class="row if-actions">' +
            '<button type="button" class="btn mini" data-act="iface-up" data-iface="' + esc(it.name) + '">拉起</button>' +
            '<button type="button" class="btn mini ghost" data-act="iface-renew" data-iface="' + esc(it.name) + '">续约</button>' +
            '<button type="button" class="btn mini danger" data-act="iface-down" data-iface="' + esc(it.name) + '" data-label="断开">断开</button>' +
          '</div>';
      return '<div class="lab-section if-card' + (it.cellModem ? ' cell-if-card' : '') + '"' +
          (it.cellModem ? ' data-cell-card="1"' : '') + '>' +
        '<div class="if-head"><span class="mono if-name">' + esc(it.name) + '</span>' +
          '<span class="status-pill ' + (up ? 'on' : '') + '">' + (up ? 'UP' : (it.available ? 'DOWN' : 'N/A')) + '</span>' +
          '<span class="grow"></span><span class="mono">' + esc((it.proto || '').toUpperCase()) + '</span></div>' +
        '<div class="if-meta"><span class="mono">' + esc(it.device || '—') + '</span>' +
          '<span>' + esc(ips) + '</span>' + (it.gateway ? '<span>GW ' + esc(it.gateway) + '</span>' : '') + '</div>' +
        ((it.dns && it.dns.length) ? '<div class="if-meta"><span>DNS ' + esc(it.dns.join(' ')) + '</span></div>' : '') +
        '<div class="if-meta mono"><span>RX ' + fmtBytes(it.rxBytes) + '</span><span>TX ' + fmtBytes(it.txBytes) + '</span>' +
          (it.linkSpeed ? '<span>' + esc(it.linkSpeed) + '</span>' : '') +
          (it.uptime ? '<span>' + shortDur(it.uptime) + '</span>' : '') + '</div>' +
        actions + '</div>';
    }).join('');
    return '<div class="lab-section"><div class="subblock-title">网络接口</div>' +
      '<div class="note">来自 ubus network.interface dump；断开 LAN 会立即中断管理连接。</div></div>' +
      (rows || '<div class="empty">未读取到接口</div>');
  }

  function consoleWireless(c) {
    const w = c.wireless || {};
    if (!w.present || (!(w.radios || []).length && !(w.ifaces || []).length)) {
      return '<div class="lab-section">' + sectionHead('wifi', '无线', '未检测到硬件') +
        '<div class="empty">设备没有无线射频（/sys/class/ieee80211 为空），无线配置保持只读。</div></div>';
    }
    const radios = (w.radios || []).map((r) =>
      '<div class="list-row"><div class="grow"><div>' + esc(r.name) + ' · ' + esc(r.band || '') + '</div>' +
      '<div class="mono">' + esc(r.section) + ' · CH ' + esc(r.channel || '—') + ' · ' + esc(r.width || '') + '</div></div>' +
      '<span class="status-pill ' + (r.active ? 'on' : '') + '">' + (r.active ? 'ON' : 'OFF') + '</span></div>'
    ).join('');
    const ifaces = (w.ifaces || []).map((f) => {
      const sec = esc(f.section);
      return '<div class="lab-section if-card">' +
        '<div class="if-head"><span class="mono if-name">' + esc(f.ssid || f.section) + '</span>' +
          '<span class="status-pill ' + (f.active ? 'on' : '') + '">' + (f.active ? 'ON' : 'OFF') + '</span>' +
          '<span class="grow"></span><span class="mono">' + esc(f.mode || 'ap') + '</span></div>' +
        '<div class="if-meta mono"><span>' + esc(f.section) + '</span><span>CH ' + esc(f.channel || '—') + '</span>' +
          (f.clients ? '<span>' + f.clients + ' 客户端</span>' : '') + '</div>' +
        '<div class="form-grid">' +
          '<div class="field full"><label>SSID</label><input id="wifiSSID-' + sec + '" value="' + esc(f.ssid) + '" spellcheck="false"></div>' +
          '<div class="field"><label>加密</label><input id="wifiENC-' + sec + '" value="' + esc(f.encryption || '') + '" spellcheck="false"></div>' +
          '<div class="field"><label>密码</label><input id="wifiKEY-' + sec + '" type="password" placeholder="留空保持" autocomplete="new-password"></div>' +
          '<div class="field"><label>信道</label><input id="wifiCH-' + sec + '" value="' + esc(f.channel || '') + '" spellcheck="false" placeholder="auto"></div>' +
          '<div class="field"><label>状态</label><label class="check-row" style="height:27px"><input type="checkbox" id="wifiOFF-' + sec + '"' + (f.disabled ? ' checked' : '') + '> 停用</label></div>' +
        '</div>' +
        '<div class="form-actions"><button type="button" class="btn mini primary" data-act="wifi-save" data-sec="' + sec + '" data-radio="' + esc(f.radio || '') + '">保存并重载</button></div>' +
      '</div>';
    }).join('');
    return '<div class="lab-section">' + sectionHead('wifi', '射频', (w.radios || []).length + ' 个') +
      (radios || '<div class="empty">无射频</div>') + '</div>' +
      (ifaces || '<div class="lab-section"><div class="empty">无无线接口</div></div>') +
      '<div class="note">保存会写入 wireless 并执行 wifi reload，无线客户端会短暂掉线。</div>';
  }

  function consolePlugins(c) {
    const rows = (c.plugins || []).map((p) => {
      const toggle = p.init
        ? '<button type="button" class="switch' + (p.running ? ' on' : '') + '" data-act="svc-toggle" data-name="' + esc(p.init) + '" data-on="' + (p.running ? 1 : 0) + '" title="启动 / 停止"></button>'
        : '';
      return '<div class="list-row"><div class="grow"><div>' + esc(p.name) + (p.version ? ' <span class="mono">' + esc(p.version) + '</span>' : '') + '</div>' +
        '<div class="mono">' + esc(p.package || p.init || p.config || '') + '</div></div>' +
        '<span class="status-pill ' + (p.running ? 'on' : '') + '">' + (p.running ? 'RUN' : (p.enabled ? 'STOP' : 'OFF')) + '</span>' +
        toggle + '</div>';
    }).join('');
    return '<div class="lab-section">' + sectionHead('bolt', '插件与服务', (c.plugins || []).length + ' 项') +
      (rows || '<div class="empty">未识别到插件</div>') +
      '<div class="note">开关通过 /etc/init.d 启停并设置开机自启；状态来自 init 脚本与 ubus。</div></div>';
  }

  function consolePackages(c) {
    const filter = (state.pkgFilter || '').toLowerCase();
    const all = c.packages || [];
    const shown = all.filter((p) => !filter || p.name.toLowerCase().indexOf(filter) >= 0).slice(0, 400);
    const rows = shown.map((p) =>
      '<div class="pkg-row" data-pkg="' + esc(p.name.toLowerCase()) + '"><span class="mono">' + esc(p.name) + '</span><span class="grow"></span><span class="mono pkg-ver">' + esc(p.version) + '</span></div>'
    ).join('');
    return '<div class="lab-section">' +
      sectionHead('terminal', '软件包', c.packageCount + ' 个已安装') +
      '<div class="row" style="margin-top:8px"><input id="pkgFilter" value="' + esc(state.pkgFilter) + '" placeholder="搜索包名" spellcheck="false">' +
      '<button type="button" class="btn mini ghost" data-act="pkg-clear">清空</button></div>' +
      '<div class="pkg-list" style="margin-top:7px">' + (rows || '<div class="empty">没有匹配的软件包</div>') + '</div>' +
      '<div class="note" style="margin-top:5px">支持 apk 与 opkg 固件；列表来自本机包数据库。</div></div>';
  }

  function consoleDHCP(c) {
    const d = c.dhcp || {};
    const pools = (d.pools || []).map((p) => {
      const sec = esc(p.section);
      return '<div class="lab-section if-card">' +
        '<div class="if-head"><span class="mono if-name">' + esc(p.interface || p.section) + '</span>' +
          '<span class="status-pill ' + (p.ignore ? '' : 'on') + '">' + (p.ignore ? 'OFF' : 'SERVER') + '</span>' +
          '<span class="grow"></span><span class="mono">' + sec + '</span></div>' +
        '<div class="form-grid">' +
          '<div class="field"><label>起始偏移</label><input id="poolStart-' + sec + '" value="' + esc(p.start) + '" inputmode="numeric"></div>' +
          '<div class="field"><label>数量</label><input id="poolLimit-' + sec + '" value="' + esc(p.limit) + '" inputmode="numeric"></div>' +
          '<div class="field full"><label>租期</label><input id="poolLease-' + sec + '" value="' + esc(p.leaseTime) + '" placeholder="12h"></div>' +
        '</div>' +
        '<div class="form-actions"><button type="button" class="btn mini primary" data-act="pool-save" data-sec="' + sec + '">保存并重启 DHCP</button></div>' +
      '</div>';
    }).join('');
    const hosts = (d.staticHosts || []).map((h) =>
      '<div class="list-row"><div class="grow"><div>' + esc(h.name || '静态主机') + '</div><div class="mono">' + esc(h.mac) + ' · ' + esc(h.section) + '</div></div>' +
      '<span class="mono">' + esc(h.ip) + '</span></div>'
    ).join('');
    const leases = (d.leases || []).map((l) =>
      '<div class="list-row"><div class="grow"><div>' + esc(l.hostname || '未命名') + '</div><div class="mono">' + esc(l.mac) + '</div></div>' +
      '<span class="mono">' + esc(l.ip) + '</span>' +
      '<button type="button" class="icon-xs" data-act="lease-block" data-mac="' + esc(l.mac) + '" data-label="拉黑" title="断开并拉黑"><svg class="i"><use href="#i-shield"/></svg></button></div>'
    ).join('');
    return '<div class="lab-section">' + sectionHead('dhcp', 'DHCP 地址池', (d.pools || []).length + ' 个') +
      '<div class="note">OpenWrt 的起始值是相对于接口地址的偏移，例如 100 + 192.168.1.0 = 192.168.1.100。</div></div>' +
      (pools || '<div class="lab-section"><div class="empty">未发现 DHCP 池</div></div>') +
      '<div class="lab-section"><div class="subblock-title">静态租约</div>' + (hosts || '<div class="empty">没有静态租约</div>') + '</div>' +
      '<div class="lab-section"><div class="subblock-title">活动租约</div>' + (leases || '<div class="empty">没有活动租约</div>') + '</div>';
  }

  function consoleFirewall(c) {
    const f = c.firewall || {};
    const zones = (f.zones || []).map((z) =>
      '<div class="list-row"><div class="grow"><div>' + esc(z.name) + (z.masq ? ' <span class="pill">MASQ</span>' : '') + '</div>' +
      '<div class="mono">' + esc((z.networks || []).join(' ')) + '</div></div>' +
      '<span class="mono zone-io">' + esc(z.input) + ' → ' + esc(z.output) + '</span></div>'
    ).join('');
    return '<div class="lab-section">' + sectionHead('shield', '防火墙', (f.zones || []).length + ' 个区域') +
      '<div class="kv"><span><b>转发规则</b> ' + (f.ruleCount || 0) + '</span>' +
      '<span><b>端口映射</b> ' + (f.redirectCount || 0) + '</span>' +
      '<span><b>FullCone</b> ' + (f.fullCone ? 'ON' : 'OFF') + '</span></div>' +
      (zones || '<div class="empty">无区域信息</div>') + '</div>' +
      '<div class="lab-section"><div class="setting-row"><div class="grow"><div class="setting-title">UPnP / NAT-PMP</div>' +
      '<div class="setting-sub">' + (f.upnpInstalled ? esc((f.upnpConfigName || 'upnpd') + '.' + (f.upnpSection || '')) : '未安装 upnpd / miniupnpd') + '</div></div>' +
      (f.upnpInstalled ? '<button type="button" class="switch' + (f.upnpRunning ? ' on' : '') + '" data-act="upnp-toggle" data-on="' + (f.upnpRunning ? 1 : 0) + '"></button>' : '') +
      '</div><div class="note" style="margin-top:6px">UPnP 由局域网设备自动申请端口映射，按需开启。</div></div>';
  }

  function consoleConfig(c) {
    const list = state.cfgList || c.configs || [];
    const options = list.map((n) => '<option value="' + esc(n) + '"' + (n === state.cfgName ? ' selected' : '') + '>' + esc(n) + '</option>').join('');
    let body = '<div class="row" style="margin-top:2px"><select id="cfgSelect">' + options + '</select></div>';
    if (state.cfgLoading) {
      body += '<div class="empty">正在读取配置…</div>';
    } else if (!state.cfgVals) {
      body += '<div class="empty">选择配置以查看可编辑选项</div>';
    } else {
      const secs = Object.keys(state.cfgVals);
      if (!secs.length) body += '<div class="empty">该配置为空</div>';
      body += secs.map((sec) => {
        const fields = state.cfgVals[sec];
        const typ = fields['.type'] || '';
        const opts = Object.keys(fields).filter((k) => k.charAt(0) !== '.').map((opt) => {
          const raw = fields[opt];
          const isList = Array.isArray(raw);
          const val = isList ? raw.join(', ') : String(raw == null ? '' : raw);
          return '<div class="opt-row"><span class="opt-key mono" title="' + esc(opt) + '">' + esc(opt) + (isList ? ' <span class="pill">list</span>' : '') + '</span>' +
            '<input class="opt-val" data-sec="' + esc(sec) + '" data-opt="' + esc(opt) + '" data-list="' + (isList ? 1 : 0) + '" value="' + esc(val) + '" spellcheck="false">' +
            '<button type="button" class="icon-xs" data-act="cfg-save" title="保存"><svg class="i"><use href="#i-check"/></svg></button>' +
            '<button type="button" class="icon-xs" data-act="cfg-del" data-sec="' + esc(sec) + '" data-opt="' + esc(opt) + '" data-label="删除" title="删除"><svg class="i"><use href="#i-trash"/></svg></button></div>';
        }).join('');
        return '<div class="cfg-sec"><div class="cfg-sec-head"><span class="mono">' + esc(sec) + '</span><span class="pill">' + esc(typ) + '</span></div>' + (opts || '<div class="note">无选项</div>') + '</div>';
      }).join('');
      body += '<div class="lab-section" style="margin-top:8px"><div class="subblock-title">新增选项</div>' +
        '<div class="form-grid">' +
          '<div class="field"><label>段 ID</label><input id="cfgNewSec" placeholder="wan" spellcheck="false"></div>' +
          '<div class="field"><label>选项</label><input id="cfgNewOpt" placeholder="metric" spellcheck="false"></div>' +
          '<div class="field full"><label>值</label><input id="cfgNewVal" placeholder="10" spellcheck="false"></div>' +
        '</div><div class="form-actions"><button type="button" class="btn mini primary" data-act="cfg-add">写入</button></div></div>';
    }
    return '<div class="lab-section">' + sectionHead('key', 'UCI 配置', state.cfgName || '') + body +
      '<div class="note" style="margin-top:6px">每个选项保存后立即 commit；未知选项请谨慎修改。</div></div>';
  }

  function consoleLogs() {
    return '<div class="lab-section">' + sectionHead('search', '系统日志', state.logKind === 'kernel' ? 'dmesg' : 'logread') +
      '<div class="form-grid" style="margin-top:7px">' +
        '<div class="field"><label>来源</label><select id="logKind"><option value="system"' + (state.logKind === 'system' ? ' selected' : '') + '>system</option><option value="kernel"' + (state.logKind === 'kernel' ? ' selected' : '') + '>kernel</option></select></div>' +
        '<div class="field"><label>行数</label><select id="logLines"><option>80</option><option>160</option><option>300</option></select></div>' +
        '<div class="field full"><label>过滤</label><input id="logFilter" value="' + esc(state.logFilter) + '" placeholder="dnsmasq / wan" spellcheck="false"></div>' +
      '</div>' +
      '<div class="form-actions"><button type="button" class="btn mini primary" data-act="log-run"' + (state.logLoading ? ' disabled' : '') + '>' + (state.logLoading ? '读取中…' : '读取') + '</button>' +
      '<button type="button" class="btn mini ghost" data-act="log-clear">清空</button></div>' +
      '<pre class="term" style="height:260px;margin-top:8px">' + esc(state.logOut || '') + '</pre>' +
    '</div>';
  }

  function consoleProcesses(c) {
    const rows = (c.processes || []).map((p) =>
      '<div class="pkg-row"><span class="mono proc-pid">' + p.pid + '</span><span class="mono proc-user">' + esc(p.user) + '</span>' +
      '<span class="mono proc-vsz">' + (p.vsz ? Math.round(p.vsz) + 'K' : '') + '</span><span class="proc-cmd">' + esc(p.command) + '</span></div>'
    ).join('');
    return '<div class="lab-section">' + sectionHead('terminal', '进程', (c.processes || []).length + ' 项') +
      (rows || '<div class="empty">未读取到进程</div>') + '</div>';
  }

  async function fetchStorage() {
    const st = state.storage;
    if (!state.connected) {
      st.err = '未连接设备：连接后才能识别可移动存储。';
      st.view = null;
      renderConsole();
      return;
    }
    if (MOCK) {
      st.err = '';
      st.view = mockStorageView();
      renderConsole();
      return;
    }
    if (!state.api || !state.api.GetStorageDevices || st.loading) return;
    st.loading = true;
    st.err = '';
    renderConsole();
    try {
      st.view = await state.api.GetStorageDevices();
    } catch (e) {
      st.err = errText(e);
    } finally {
      st.loading = false;
      renderConsole();
    }
  }

  function storageDeviceByPath(path) {
    const list = (state.storage.view && state.storage.view.devices) || [];
    return list.find((d) => d.device === path) || null;
  }

  async function mountStorage(dev) {
    const st = state.storage;
    const host = (state.info && state.info.host) || '';
    if (!host) { toast('缺少当前设备地址，请重新连接', 'info'); return; }
    if (st.busy) return;
    st.busy = true;
    st.err = '';
    renderConsole();
    try {
      st.view = await state.api.MountStorage({
        device: dev.device, mountPoint: dev.mountTarget,
        confirmText: host, confirmed: true,
      });
      toast('已挂载 ' + dev.device, 'ok');
    } catch (e) {
      st.err = errText(e);
      toastError(e);
    } finally {
      st.busy = false;
      renderConsole();
    }
  }

  async function unmountStorage(dev) {
    const st = state.storage;
    const host = (state.info && state.info.host) || '';
    if (!host) { toast('缺少当前设备地址，请重新连接', 'info'); return; }
    if (st.busy) return;
    st.busy = true;
    st.err = '';
    renderConsole();
    try {
      st.view = await state.api.UnmountStorage({
        device: dev.device, mountPoint: dev.mountPoint,
        confirmText: host, confirmed: true,
      });
      toast('已卸载 ' + dev.device, 'ok');
    } catch (e) {
      st.err = errText(e);
      toastError(e);
    } finally {
      st.busy = false;
      renderConsole();
    }
  }

  function consoleStorage(c) {
    const st = state.storage;
    const df = (c.storage || []).map((s) =>
      '<div class="list-row"><div class="grow"><div class="mono">' + esc(s.filesystem) + '</div><div class="mono">' + esc(s.mount) + '</div></div>' +
      '<span class="mono">' + esc(s.used) + ' / ' + esc(s.size) + ' · ' + esc(s.usePct) + '</span></div>'
    ).join('');
    const mounts = (c.mounts || []).map((m) =>
      '<div class="list-row"><span class="mono">' + esc(m.device) + '</span><span class="grow"></span>' +
      '<span class="mono">' + esc(m.mount) + ' · ' + esc(m.fs) + '</span></div>'
    ).join('');

    const view = st.view;
    let devices = '';
    if (st.loading) devices = '<div class="note">正在读取块设备…</div>';
    else if (st.err) devices = '<div class="note bad">' + esc(st.err) + '</div>';
    else if (!view) devices = '<div class="note">切换到本页后会自动识别 U 盘与移动硬盘。</div>';
    else if (!view.devices || !view.devices.length) devices = '<div class="note">' + esc(view.note || '没有识别到块设备。') + '</div>';
    else {
      devices = view.devices.map((d) => {
        const mounted = !!d.mounted;
        const canMount = !mounted && !d.reason && d.fsSupported && !d.protected;
        const status = mounted ? '<span class="usb-status on">已挂载</span>' : '<span class="usb-status">未挂载</span>';
        const target = mounted ? ('当前挂载点 ' + d.mountPoint) : ('未挂载 · 目标 ' + d.mountTarget);
        let action;
        if (d.protected) {
          const label = mounted ? '卸载' : '挂载';
          action = '<button type="button" class="btn mini ghost" disabled title="' + esc(d.blockedReason || '系统运行盘，禁止操作') + '">' + label + '</button>';
        } else if (mounted) {
          action = '<button type="button" class="btn mini ghost" data-act="storage-unmount" data-dev="' + esc(d.device) + '"' + (st.busy ? ' disabled' : '') + '>卸载</button>';
        } else {
          action = '<button type="button" class="btn mini primary" data-act="storage-mount" data-dev="' + esc(d.device) + '"' + (canMount && !st.busy ? '' : ' disabled') + '>挂载</button>';
        }
        return '<div class="usb-card' + (d.protected ? ' sys' : '') + '">' +
          '<div class="usb-head"><span class="mono usb-dev">' + esc(d.device) + '</span>' +
          (d.label ? '<span class="usb-label">' + esc(d.label) + '</span>' : '') +
          (d.removable ? '<span class="usb-rem">可移动</span>' : '') + status + '</div>' +
          '<div class="usb-role' + (d.protected ? ' sys' : '') + '">' + esc(d.role || '块设备') + '</div>' +
          '<div class="usb-meta">' + esc(d.fsType || '未知文件系统') + ' · ' + esc(instBytes((d.sizeKb || 0) * 1024)) +
          (d.uuid ? ' · ' + esc(d.uuid) : '') + '</div>' +
          '<div class="usb-meta">' + esc(target) + '</div>' +
          (d.protected && d.blockedReason ? '<div class="note bad">' + esc(d.blockedReason) + '</div>' : '') +
          (!d.protected && d.reason ? '<div class="note bad">' + esc(d.reason) + '</div>' : '') +
          '<div class="usb-actions">' + action + '</div>' +
          '</div>';
      }).join('');
    }

    return '<div class="lab-section">' + sectionHead('down', '存储使用', (c.storage || []).length + ' 个卷') + (df || '<div class="empty">无数据</div>') + '</div>' +
      '<div class="lab-section">' + sectionHead('plug', '块设备', '全部磁盘与分区，含系统运行盘') +
      '<div class="row-actions"><button type="button" class="btn mini ghost" data-act="storage-refresh"' + (st.loading ? ' disabled' : '') + '>刷新识别</button></div>' +
      devices + '</div>' +
      '<div class="lab-section"><div class="subblock-title">挂载点</div>' + (mounts || '<div class="empty">无挂载信息</div>') + '</div>';
  }

  function setOpMsg(key, tone, text, detail) {
    state.opMsgs[key] = { tone: tone || 'ok', text: text || '', detail: detail || '' };
  }

  function opMsgHtml(key) {
    const m = state.opMsgs[key];
    if (!m) return '';
    return '<div class="op-result ' + esc(m.tone) + '">' + esc(m.text) + '</div>' +
      (m.detail ? '<details class="op-detail"><summary>详情</summary><pre class="term">' + esc(m.detail) + '</pre></details>' : '');
  }

  function humanErrText(raw) {
    const s = String(raw || '').trim();
    if (!s) return '操作没有完成，请重试。';
    if (/需要重新输入密码|凭据已失效/.test(s)) return '登录凭据已失效，请重新输入密码。';
    if (/timeout|超时|timed out/i.test(s)) return '设备没有响应，请稍后再试。';
    if (/permission denied|not permitted/i.test(s)) return '设备拒绝写入，请确认账户权限。';
    if (/no space|空间不足/i.test(s)) return '设备存储空间不足。';
    if (/connection refused|connection reset|no route|unreachable|connection (closed|lost)/i.test(s)) return '与设备的连接不稳定，请检查网络。';
    if (/not found|command not found/i.test(s)) return '设备上缺少对应组件，无法执行这个操作。';
    return s.length <= 100 ? s : '操作没有完成，展开详情查看具体原因。';
  }

  function opFail(key, e) {
    const raw = errText(e);
    setOpMsg(key, 'bad', humanErrText(raw), raw);
    if (/需要重新输入密码|凭据已失效/.test(raw)) toastError(e);
    renderLab();
  }

  function captureToolDrafts() {
    const val = (id) => {
      const el = document.getElementById(id);
      return el ? el.value : null;
    };
    const pfExt = val('pfExt');
    if (pfExt !== null) {
      state.pfDraft = {
        name: val('pfName') || '',
        proto: val('pfProto') || 'tcp',
        ext: pfExt,
        destPort: val('pfDestPort') || '',
        destIP: val('pfDestIP') || '',
      };
    }
    const dns = val('dnsInput');
    if (dns !== null) state.dnsDraft = dns;
  }

  function renderTools() {
    const host = $('#labContent');
    if (!host) return;
    captureToolDrafts();
    const scroll = $('#labScroll');
    const top = scroll ? scroll.scrollTop : 0;
    const lab = state.lab;
    const info = (lab && lab.info) || state.info || {};
    $('#labCrumb').textContent = [info.hostname, info.model].filter(Boolean).join(' · ') || 'advanced';

    if (!lab) {
      host.innerHTML = state.labLoading
        ? '<div class="empty">正在读取设备配置…</div>'
        : '<div class="empty">实验室数据不可用，请检查连接后重试。</div>';
      if (scroll) scroll.scrollTop = top;
      return;
    }

    const caps = lab.caps || {};
    const parts = [];
    parts.push(deviceStrip(lab, info));

    /* --- port forwarding --- */
    const pfRows = (lab.portForwards || []).map((p) => {
      return '<div class="pf-row">' +
        '<div class="pf-main"><div class="pf-route"><span class="proto">' + esc(p.proto || 'tcp') + '</span>' +
        ':' + esc(p.extPort) + '<span class="arrow">→</span>' + esc(p.destIp) + ':' + esc(p.destPort) + '</div>' +
        '<div class="pf-meta">' + esc(p.name || '未命名规则') + '</div></div>' +
        '<div class="pf-actions">' +
        '<button type="button" class="switch' + (p.enabled ? ' on' : '') + '" data-act="pf-toggle" data-id="' + esc(p.id) + '" data-on="' + (p.enabled ? 1 : 0) + '" title="启用 / 停用"></button>' +
        '<button type="button" class="icon-xs" data-act="pf-del" data-id="' + esc(p.id) + '" data-label="删除" title="删除规则"><svg class="i"><use href="#i-trash"/></svg></button>' +
        '</div></div>';
    }).join('');

    const pfD = state.pfDraft || {};
    const pfProto = pfD.proto || 'tcp';
    const pfForm = state.pfFormOpen ? (
      '<div class="form-grid">' +
      '<div class="field"><label>名称</label><input id="pfName" placeholder="NAS 管理" spellcheck="false" value="' + esc(pfD.name || '') + '"></div>' +
      '<div class="field"><label>协议</label><select id="pfProto">' +
      '<option value="tcp"' + (pfProto === 'tcp' ? ' selected' : '') + '>TCP</option>' +
      '<option value="udp"' + (pfProto === 'udp' ? ' selected' : '') + '>UDP</option>' +
      '<option value="tcp udp"' + (pfProto === 'tcp udp' ? ' selected' : '') + '>TCP + UDP</option></select></div>' +
      '<div class="field"><label>外部端口</label><input id="pfExt" inputmode="numeric" placeholder="8443" spellcheck="false" value="' + esc(pfD.ext || '') + '"></div>' +
      '<div class="field"><label>内网端口</label><input id="pfDestPort" inputmode="numeric" placeholder="443" spellcheck="false" value="' + esc(pfD.destPort || '') + '"></div>' +
      '<div class="field full"><label>内网 IP</label><input id="pfDestIP" placeholder="192.168.1.10" spellcheck="false" value="' + esc(pfD.destIP || defaultDestIP()) + '"></div>' +
      '</div>' +
      '<div class="form-actions"><button type="button" class="btn mini ghost" data-act="pf-cancel">取消</button>' +
      '<button type="button" class="btn mini primary" data-act="pf-add">添加规则</button></div>'
    ) : '';

    parts.push(acc('pf', 'wan', 'NAT 端口转发', opMsgHtml('pf') + pfRows + pfForm +
      '<div class="row" style="margin-top:7px;justify-content:flex-end">' +
      (state.pfFormOpen ? '' : '<button type="button" class="btn mini" data-act="pf-new"><svg class="i"><use href="#i-plus"/></svg>新增映射</button>') +
      '</div>' +
      '<div class="note" style="margin-top:6px">写回 firewall redirect 并重载防火墙；反射（NAT loopback）默认启用。</div>',
      (lab.portForwards || []).length + ' 条'));

    /* --- probes --- */
    const targets = ['1.1.1.1', '8.8.8.8', '192.168.100.1'];
    const pv = probeSummary(state.probeKind, state.probeOut);
    const probeBody = '<div class="chips">' + targets.map((t) =>
      '<button type="button" class="chip' + (state.probeTarget === t ? ' active' : '') + '" data-target="' + t + '">' + t + '</button>'
    ).join('') + '</div>' +
      '<div class="probe-row"><input id="probeTarget" value="' + esc(state.probeTarget) + '" spellcheck="false" placeholder="目标 IP 或域名">' +
      '<button type="button" class="btn mini" data-act="probe-ping"' + (state.probeBusy ? ' disabled' : '') + '>Ping</button>' +
      '<button type="button" class="btn mini" data-act="probe-trace"' + (state.probeBusy ? ' disabled' : '') + '>Trace</button></div>' +
      (state.probeBusy ? '<div class="note" style="margin-top:6px">探测运行中…</div>' : '') +
      (pv ? '<div class="op-result ' + pv.tone + '">' + esc(pv.text) + '</div>' : '') +
      (state.probeOut
        ? '<details class="op-detail"><summary>原始输出</summary>' + termBlock(state.probeOut, 'probeTerm') + '</details>'
        : termBlock(null, 'probeTerm')) +
      '<div class="term-foot"><span class="grow">ICMP ping · traceroute</span><button type="button" class="link-btn" data-act="probe-clear">清空</button></div>';
    parts.push(acc('probe', 'search', '网络探针', probeBody, state.probeOut ? (pv ? pv.text : 'result') : 'idle'));

    /* --- DNS & fullcone --- */
    const servers = (lab.dns && lab.dns.servers) || [];
    const effective = (lab.dns && lab.dns.effective) || [];
    const dnsDraft = state.dnsDraft !== null ? state.dnsDraft : servers.join(', ');
    const dnsBody = opMsgHtml('dns') + '<div class="subblock">' +
      '<div class="subblock-title">上游 DNS</div>' +
      ((servers.length ? servers.map((s) => '<div class="list-row"><span class="mono">' + esc(s) + '</span><span class="grow"></span><span class="status-pill on">静态</span></div>').join('') :
        '<div class="note">未显式设置，使用上游自动下发。</div>')) +
      (effective.length ? '<div class="subblock-title" style="margin-top:4px">系统解析</div>' +
        '<div class="kv">' + effective.map((s) => '<span class="mono">' + esc(s) + '</span>').join('') + '</div>' : '') +
      '</div>' +
      '<div class="row" style="margin-top:7px"><input id="dnsInput" value="' + esc(dnsDraft) + '" placeholder="1.1.1.1, 8.8.8.8" spellcheck="false">' +
      '<button type="button" class="btn mini" data-act="dns-save">保存</button></div>' +
      '<div class="note" style="margin-top:5px">最多 4 个地址，写入 dnsmasq server 列表并重启 DHCP。' +
      (caps.doh ? ' 已检测到 https-dns-proxy。' : ' 未检测到 DoH 代理，当前为明文上游。') + '</div>' +
      '<div class="setting-row"><div class="grow"><div class="setting-title">FullCone NAT</div>' +
      '<div class="setting-sub">firewall.defaults.fullcone' + (caps.fullcone ? '' : ' · 固件未显式声明') + '</div></div>' +
      '<button type="button" class="switch' + (lab.fullcone ? ' on' : '') + '" data-act="fc-toggle" title="切换 FullCone"></button></div>';
    parts.push(acc('dns', 'shield', 'DoH / DNS 与防火墙', dnsBody, lab.fullcone ? 'FullCone' : 'NAT'));

    /* --- DDNS --- */
    const dd = lab.ddns || { installed: false, services: [] };
    let ddnsBody = '';
    if (!dd.installed) {
      ddnsBody = '<div class="empty">未安装 ddns-scripts（/etc/init.d/ddns）</div>';
    } else if (!(dd.services || []).length) {
      ddnsBody = '<div class="empty">已安装 DDNS，但尚未配置服务</div>';
    } else {
      ddnsBody = dd.services.map((s) =>
        '<div class="list-row"><div class="grow"><div>' + esc(s.domain || s.name) + '</div>' +
        '<div class="mono">' + esc(s.name) + '</div></div>' +
        '<span class="status-pill ' + (s.running ? 'on' : '') + '">' + (s.running ? 'RUN' : 'STOP') + '</span></div>'
      ).join('');
    }
    parts.push(acc('ddns', 'proxy', 'DDNS 状态', ddnsBody, dd.installed ? (dd.services || []).length + ' 服务' : '未安装'));

    /* --- services / blocklist --- */
    const svcRows = (lab.services || []).map((s) => {
      const controllable = ['sqm', 'AdGuardHome', 'adguardhome', 'https-dns-proxy', 'passwall', 'shellcrash', 'openclash', 'clash', 'mwan3'].indexOf(s.key) >= 0;
      return '<div class="list-row"><div class="grow"><div>' + esc(s.name) + '</div>' +
        '<div class="mono">' + esc(s.key) + '</div></div>' +
        '<span class="status-pill ' + (s.running ? 'on' : '') + '">' + (s.running ? 'RUN' : 'STOP') + '</span>' +
        (controllable ? '<button type="button" class="switch' + (s.running ? ' on' : '') + '" data-act="svc-toggle" data-name="' + esc(s.key) + '" data-on="' + (s.running ? 1 : 0) + '" title="启用 / 停用"></button>' : '') +
        '</div>';
    }).join('');

    const blockRows = (lab.blocked || []).length
      ? (lab.blocked || []).map((b) =>
        '<div class="list-row"><span class="mono">' + esc(b.mac) + '</span><span class="grow"></span>' +
        '<button type="button" class="btn mini ghost" data-act="unblock" data-mac="' + esc(b.mac) + '">解除</button></div>'
      ).join('')
      : '<div class="empty">黑名单为空</div>';

    const fwBody = opMsgHtml('svc') + '<div class="subblock-title">已拉黑设备</div>' + blockRows +
      '<div class="subblock-title" style="margin-top:9px">服务</div>' + (svcRows || '<div class="empty">无服务信息</div>') +
      '<div class="note" style="margin-top:6px">开关仅对 sqm、AdGuard、DoH、代理与 mwan3 开放；状态来自 ubus service list。</div>';
    parts.push(acc('svc', 'terminal', '防火墙与服务', fwBody, (lab.services || []).length + ' 项'));

    /* --- SSH terminal --- */
    const cv = cmdVerdict(state.cmdOut);
    const sshBody = '<div class="probe-row"><input id="sshInput" value="' + esc(state.cmdLast) + '" placeholder="uci show network" spellcheck="false">' +
      '<button type="button" class="btn mini" data-act="cmd-run">运行</button>' +
      '<button type="button" class="btn mini ghost" data-act="cmd-clear">清空</button></div>' +
      (cv ? '<div class="op-result ' + cv.tone + '">' + esc(cv.text) + '</div>' : '') +
      (state.cmdOut
        ? '<details class="op-detail" open><summary>命令输出</summary>' + termBlock(state.cmdOut, 'cmdTerm') + '</details>'
        : termBlock(null, 'cmdTerm')) +
      '<div class="term-foot"><span class="grow">以 SSH 登录用户身份执行 · Enter 运行 · ↑↓ 历史</span><span class="mono">' + esc(info.user || 'root') + '@' + esc(info.host || '') + '</span></div>';
    parts.push(acc('ssh', 'terminal', 'SSH 终端', sshBody, state.cmdOut ? 'last: ' + state.cmdLast.slice(0, 18) : 'shell'));

    /* --- link / hardware --- */
    const wan = lab.wan || {};
    let linkBody = opMsgHtml('link') + '<div class="kv">' +
      '<span><b>接口</b> ' + esc(wan.device || '—') + '</span>' +
      '<span><b>协议</b> ' + esc((wan.proto || '—').toUpperCase()) + '</span>' +
      '<span><b>网关</b> ' + esc(wan.gateway || '—') + '</span>' +
      '<span><b>链路</b> ' + esc(wan.linkSpeed || '—') + '</span></div>' +
      '<div class="kv" style="margin-top:4px"><span><b>DNS</b> ' + esc((wan.dns || []).join('  ') || '—') + '</span></div>';
    if (lab.wireless && lab.wireless.present) {
      linkBody += '<div class="setting-row"><div class="grow"><div class="setting-title">无线</div>' +
        '<div class="setting-sub">' + esc(lab.wireless.ssid || '未启用') + (lab.wireless.channel ? ' · CH' + esc(lab.wireless.channel) : '') + '</div></div>' +
        '<span class="status-pill ' + (lab.wireless.enabled ? 'on' : '') + '">' + (lab.wireless.enabled ? 'ON' : 'OFF') + '</span></div>';
    }
    if (lab.cellular && (lab.cellular.present || lab.cellular.installed)) {
      const cm = lab.cellular;
      const cmNoSim = /ABSENT|NOT INSERTED|NO SIM|MISS|ERROR|FAIL/i.test(cm.simStatus || '');
      const cmSub = [cm.model, cm.interface, cmNoSim ? '未插卡' : (cm.connected ? '已连接' : '未连接')]
        .filter(Boolean).join(' · ') || (cm.count + ' 个模组');
      linkBody += '<div class="setting-row"><div class="grow"><div class="setting-title">蜂窝网络</div>' +
        '<div class="setting-sub">' + esc(cmSub) + '</div></div>' +
        '<button type="button" class="btn mini" data-act="cell-detail">蜂窝详情</button></div>';
    }
    if (lab.mwan && lab.mwan.installed) {
      linkBody += '<div class="subblock-title" style="margin-top:8px">MWAN3</div>';
      (lab.mwan.interfaces || []).forEach((i) => {
        linkBody += '<div class="list-row"><span class="mono">' + esc(i.name) + '</span><span class="grow"></span><span class="status-pill ' + (/online/i.test(i.status) ? 'on' : '') + '">' + esc(i.status) + '</span></div>';
      });
    }
    linkBody += '<div class="form-actions"><button type="button" class="btn mini" data-act="wan-up">拉起 WAN</button>' +
      '<button type="button" class="btn mini ghost" data-act="wan-renew">续约</button>' +
      '<button type="button" class="btn mini danger" data-act="wan-down" data-label="断开 WAN">断开 WAN</button></div>';
    parts.push(acc('link', 'router', '链路与硬件', linkBody, wan.status || '—'));

    /* --- danger --- */
    parts.push('<div class="lab-section danger-zone">' +
      sectionHead('alert', '危险操作', '3 秒内二次点击确认') +
      '<div class="danger-row"><div class="grow"><div class="danger-title">重载网络接口</div><div class="danger-sub">重新加载 network 配置，WAN/LAN 会短暂中断</div></div>' +
      '<button type="button" class="btn mini danger" data-act="reload" data-label="重载">重载</button></div>' +
      '<div class="danger-row"><div class="grow"><div class="danger-title">重启路由器</div><div class="danger-sub">立即 reboot，SSH 会话将断开</div></div>' +
      '<button type="button" class="btn mini danger" data-act="reboot" data-label="重启">重启</button></div>' +
      '<div class="danger-row"><div class="grow"><div class="danger-title">断开 SSH</div><div class="danger-sub">仅关闭桌面端会话，不修改设备</div></div>' +
      '<button type="button" class="btn mini ghost" data-act="disconnect">断开</button></div>' +
      '</div>');

    host.innerHTML = parts.join('');
    if (scroll) scroll.scrollTop = top;
  }

  function deviceStrip(lab, info) {
    const fp = lab.fingerprint || info.fingerprint || '';
    return '<div class="lab-section device-strip">' +
      '<div class="device-name"><svg class="i" style="color:var(--accent-blue)"><use href="#i-router"/></svg>' +
      esc(info.hostname || 'OpenWrt') +
      '<span class="link-badge mono">' + esc((lab.wan && lab.wan.linkSpeed) || 'SSH') + '</span></div>' +
      '<div class="device-meta">' +
      (info.model ? '<span>' + esc(info.model) + '</span>' : '') +
      (info.chipset ? '<span>' + esc(info.chipset) + '</span>' : '') +
      (info.kernel ? '<span>kernel ' + esc(info.kernel) + '</span>' : '') +
      (info.release ? '<span>' + esc(info.release) + '</span>' : '') +
      '</div>' +
      '<div class="fp"><svg class="i"><use href="#i-lock"/></svg><span title="' + esc(fp) + '">' + esc(shortFp(fp)) + '</span>' +
      '<button type="button" class="icon-xs" data-act="copy-fp" style="margin-left:auto" title="复制指纹"><svg class="i"><use href="#i-copy"/></svg></button></div>' +
      '</div>';
  }

  function defaultDestIP() {
    const cs = (state.snap && state.snap.clients) || [];
    const online = cs.find((c) => c.online);
    return online ? online.ip : '192.168.1.10';
  }

  /* ================= lab actions ================= */
  async function handleLabAction(btn) {
    const act = btn.dataset.act;
    if (!act) return;

    if (act === 'ctabs-left' || act === 'ctabs-right') {
      const tabs = $('#labContent .ctabs');
      if (tabs) {
        const step = Math.max(140, Math.round(tabs.clientWidth * 0.8));
        tabs.scrollBy({ left: act === 'ctabs-left' ? -step : step, behavior: 'smooth' });
      }
      return;
    }

    if (act === 'storage-refresh') { await fetchStorage(); return; }
    if (act === 'storage-mount') {
      const dev = storageDeviceByPath(btn.dataset.dev);
      if (!dev) { toast('设备已不在列表中，请刷新识别', 'info'); return; }
      arm(btn, '挂载', () => mountStorage(dev));
      return;
    }
    if (act === 'storage-unmount') {
      const dev = storageDeviceByPath(btn.dataset.dev);
      if (!dev) { toast('设备已不在列表中，请刷新识别', 'info'); return; }
      arm(btn, '卸载', () => unmountStorage(dev));
      return;
    }
    if (act === 'market-device-repos') {
      if (!state.connected) { toast('未连接设备：请手动填写仓库地址', 'info'); return; }
      try {
        const env = await state.api.GetMarketRepos();
        state.market.env = env;
        state.market.envErr = (env && env.note) || '';
        if (env && env.feeds && env.feeds.length) {
          state.market.reposText = env.feeds.map((f) => f.url).join('\n');
          state.market.reposTouched = true;
          toast('已带出设备仓库 ' + env.feeds.length + ' 条', 'ok');
        } else {
          toast((env && env.note) || '设备没有可用的包源', 'info');
        }
      } catch (e) { toastError(e); }
      renderMarket();
      return;
    }
    if (act === 'market-save-repos') {
      try {
        localStorage.setItem(MARKET_REPOS_KEY, marketRepoLines().join('\n'));
        toast('仓库地址已保存到本地配置', 'ok');
      } catch (e) { toastError(e); }
      return;
    }
    if (act === 'market-fetch') { await fetchMarketIndex(); return; }
    if (act === 'market-select') {
      selectMarketPkg(btn.dataset.name || '', btn.dataset.ver || '');
      return;
    }
    if (act === 'market-page') {
      const n = parseInt(btn.dataset.page, 10);
      if (!isNaN(n) && n >= 0) {
        state.market.page = n;
        updateMarketList();
        const wrap = $('#mkListWrap');
        if (wrap) wrap.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
      }
      return;
    }
    if (act === 'market-plan') { await resolveMarketPlan(); return; }
    if (act === 'market-dryrun') { await startMarketJob(); return; }
    if (act === 'market-install') { await installMarketSelected(); return; }
    if (act === 'market-cancel') {
      try {
        if (state.market.jobId) await state.api.CancelInstallJob(state.market.jobId);
        toast('已请求取消', 'info');
      } catch (e) { toastError(e); }
      return;
    }
    if (act === 'market-reset') {
      state.market.job = null;
      state.market.jobId = '';
      state.market.starting = false;
      clearTimeout(state.market.pollTimer);
      renderMarket();
      return;
    }

    const lab = state.lab;

    const afterConsoleChange = async (msg) => {
      toast(msg, 'ok', 2200);
      await fetchConsole(true);
      refreshSnapshot(false);
    };
    const reloadCfg = async (msg) => {
      toast(msg, 'ok', 2200);
      state.cfgVals = null;
      await ensureConfigData();
      refreshSnapshot(false);
    };

    if (act === 'sys-save') {
      const c = state.console;
      if (!c) return;
      const hostname = (($('#sysHost') || {}).value || '').trim();
      const timezone = (($('#sysTZ') || {}).value || '').trim();
      const zonename = (($('#sysZone') || {}).value || '').trim();
      const ntp = (($('#sysNTP') || {}).value || '').split(/[,\s]+/).map((s) => s.trim()).filter(Boolean);
      btn.disabled = true;
      try {
        await state.api.SetSystem({ section: c.systemSection, hostname, timezone, zonename, ntpSection: c.ntpSection, ntpServers: ntp });
        await afterConsoleChange('系统设置已保存');
      } catch (e) { toastError(e); btn.disabled = false; }
      return;
    }

    if (act === 'cell-detail') {
      closeLab();
      openCellView();
      return;
    }

    if (act === 'iface-up' || act === 'iface-renew' || act === 'iface-down') {
      const name = btn.dataset.iface;
      const action = act.slice(6);
      const go = async () => {
        btn.classList.add('busy');
        try {
          await state.api.InterfaceAction(name, action);
          await afterConsoleChange('接口 ' + name + ' 已' + (action === 'up' ? '拉起' : action === 'renew' ? '续约' : '断开'));
        } catch (e) { toastError(e); btn.classList.remove('busy'); }
      };
      if (action === 'down') arm(btn, '断开', go); else go();
      return;
    }

    if (act === 'wifi-save') {
      const sec = btn.dataset.sec;
      const radio = btn.dataset.radio || '';
      const ssid = (($('#wifiSSID-' + sec) || {}).value || '').trim();
      const encryption = (($('#wifiENC-' + sec) || {}).value || '').trim();
      const key = (($('#wifiKEY-' + sec) || {}).value || '');
      const channel = (($('#wifiCH-' + sec) || {}).value || '').trim();
      const disabled = !!($('#wifiOFF-' + sec) || {}).checked;
      btn.disabled = true;
      try {
        await state.api.SetWiFi({ section: sec, ssid, key, encryption: encryption || 'none', disabled, radioSection: radio, channel, width: '', txPower: '' });
        await afterConsoleChange('无线配置已保存');
      } catch (e) { toastError(e); btn.disabled = false; }
      return;
    }

    if (act === 'pool-save') {
      const sec = btn.dataset.sec;
      const start = (($('#poolStart-' + sec) || {}).value || '').trim();
      const limit = (($('#poolLimit-' + sec) || {}).value || '').trim();
      const leaseTime = (($('#poolLease-' + sec) || {}).value || '').trim();
      btn.disabled = true;
      try {
        await state.api.SetDHCPPool({ section: sec, start, limit, leaseTime });
        await afterConsoleChange('DHCP 地址池已保存');
      } catch (e) { toastError(e); btn.disabled = false; }
      return;
    }

    if (act === 'lease-block') {
      const mac = btn.dataset.mac;
      arm(btn, '拉黑', async () => {
        try {
          await state.api.SetBlocked(mac, true);
          await afterConsoleChange('已拉黑 ' + mac);
        } catch (e) { toastError(e); }
      });
      return;
    }

    if (act === 'upnp-toggle') {
      const on = btn.dataset.on === '1';
      btn.classList.add('busy');
      try {
        await state.api.SetUPnP(!on);
        await afterConsoleChange(!on ? 'UPnP 已开启' : 'UPnP 已关闭');
      } catch (e) { toastError(e); btn.classList.remove('busy'); }
      return;
    }

    if (act === 'svc-reload') {
      const name = btn.dataset.name;
      btn.classList.add('busy');
      try {
        await state.api.ReloadService(name);
        await afterConsoleChange(name + ' 已重载');
      } catch (e) { toastError(e); btn.classList.remove('busy'); }
      return;
    }

    if (act === 'cfg-save') {
      const row = btn.closest('.opt-row');
      const input = row && row.querySelector('.opt-val');
      if (!input) return;
      const isList = input.dataset.list === '1';
      const vals = input.value.split(',').map((s) => s.trim()).filter(Boolean);
      btn.disabled = true;
      try {
        if (isList) {
          await state.api.SetUCIList({ config: state.cfgName, section: input.dataset.sec, option: input.dataset.opt, values: vals });
        } else {
          await state.api.SetUCIOption({ config: state.cfgName, section: input.dataset.sec, option: input.dataset.opt, value: input.value });
        }
        await reloadCfg(input.dataset.sec + '.' + input.dataset.opt + ' 已保存');
      } catch (e) { toastError(e); btn.disabled = false; }
      return;
    }

    if (act === 'cfg-del') {
      arm(btn, '删除', async () => {
        try {
          await state.api.DeleteUCIOption(state.cfgName, btn.dataset.sec, btn.dataset.opt);
          await reloadCfg('选项已删除');
        } catch (e) { toastError(e); }
      });
      return;
    }

    if (act === 'cfg-add') {
      const sec = (($('#cfgNewSec') || {}).value || '').trim();
      const opt = (($('#cfgNewOpt') || {}).value || '').trim();
      const val = (($('#cfgNewVal') || {}).value || '');
      if (!sec || !opt) { toast('请填写段 ID 与选项名', 'error'); return; }
      btn.disabled = true;
      try {
        await state.api.SetUCIOption({ config: state.cfgName, section: sec, option: opt, value: val });
        await reloadCfg('选项已写入');
      } catch (e) { toastError(e); btn.disabled = false; }
      return;
    }

    if (act === 'log-run') { fetchLogs(); return; }
    if (act === 'log-clear') { state.logOut = ''; renderConsole(); return; }
    if (act === 'pkg-clear') {
      state.pkgFilter = '';
      renderConsole();
      return;
    }

    if (!lab && act !== 'disconnect') return;

    if (act === 'copy-fp') {
      copyText((lab && lab.fingerprint) || (state.info && state.info.fingerprint) || '', '主机指纹');
      return;
    }
    if (act === 'pf-new') {
      state.pfFormOpen = true;
      state.pfDraft = { name: '', proto: 'tcp', ext: '', destPort: '', destIP: defaultDestIP() };
      renderLab();
      return;
    }
    if (act === 'pf-cancel') {
      state.pfFormOpen = false;
      state.pfDraft = { name: '', proto: 'tcp', ext: '', destPort: '', destIP: '' };
      renderLab();
      return;
    }
    if (act === 'pf-toggle') {
      const id = btn.dataset.id;
      const on = btn.dataset.on === '1';
      btn.classList.add('busy');
      try {
        await state.api.TogglePortForward(id, !on);
        setOpMsg('pf', 'ok', !on ? '规则已启用。' : '规则已停用。');
        await fetchLab();
        await refreshSnapshot(true);
      } catch (e) { opFail('pf', e); }
      return;
    }
    if (act === 'pf-del') {
      const id = btn.dataset.id;
      arm(btn, '删除', async () => {
        try {
          await state.api.DeletePortForward(id);
          setOpMsg('pf', 'ok', '规则已删除。');
          await fetchLab();
          await refreshSnapshot(true);
        } catch (e) { opFail('pf', e); }
      });
      return;
    }
    if (act === 'pf-add') {
      const name = ($('#pfName') || {}).value || '';
      const proto = ($('#pfProto') || {}).value || 'tcp';
      const extPort = ($('#pfExt') || {}).value || '';
      const destIP = ($('#pfDestIP') || {}).value || '';
      const destPort = ($('#pfDestPort') || {}).value || '';
      state.pfDraft = { name, proto, ext: extPort, destPort, destIP };
      if (!extPort || !destIP || !destPort) { toast('请填写外部端口、内网 IP 与内网端口', 'error'); return; }
      btn.disabled = true;
      try {
        await state.api.AddPortForward({ name: name.trim(), proto, extPort: extPort.trim(), destIp: destIP.trim(), destPort: destPort.trim() });
        setOpMsg('pf', 'ok', '已添加：' + proto.toUpperCase() + ' ' + extPort.trim() + ' → ' + destIP.trim() + ':' + destPort.trim() + '。');
        await fetchLab();
        await refreshSnapshot(true);
      } catch (e) { opFail('pf', e); }
      return;
    }
    if (act === 'dns-save') {
      const val = ($('#dnsInput') || {}).value || '';
      state.dnsDraft = val;
      btn.disabled = true;
      try {
        await state.api.SetDNS(val);
        const list = val.split(/[,\s]+/).map((s) => s.trim()).filter(Boolean);
        setOpMsg('dns', 'ok', list.length ? '已保存上游 DNS：' + list.join('、') + '。' : '已清空静态上游，改用上游自动下发。');
        await fetchLab();
      } catch (e) { opFail('dns', e); }
      return;
    }
    if (act === 'fc-toggle') {
      btn.classList.add('busy');
      try {
        await state.api.SetFullCone(!lab.fullcone);
        setOpMsg('dns', 'ok', lab.fullcone ? 'FullCone NAT 已关闭。' : 'FullCone NAT 已开启。');
        await fetchLab();
        await refreshSnapshot(true);
      } catch (e) { opFail('dns', e); }
      return;
    }
    if (act === 'svc-toggle') {
      const name = btn.dataset.name;
      const on = btn.dataset.on === '1';
      btn.classList.add('busy');
      try {
        await state.api.ToggleService(name, !on);
        setOpMsg('svc', 'ok', name + (!on ? ' 已启用。' : ' 已停用。'));
        await refreshSnapshot(true);
        if (state.labTab === 'tools') await fetchLab(); else await fetchConsole(true);
      } catch (e) { opFail('svc', e); }
      return;
    }
    if (act === 'unblock') {
      try {
        await state.api.SetBlocked(btn.dataset.mac, false);
        setOpMsg('svc', 'ok', '已解除拉黑 ' + btn.dataset.mac + '。');
        await fetchLab();
        await refreshSnapshot(true);
      } catch (e) { opFail('svc', e); }
      return;
    }
    if (act === 'probe-ping' || act === 'probe-trace') {
      runProbe(act === 'probe-ping' ? 'ping' : 'trace');
      return;
    }
    if (act === 'probe-clear') { state.probeOut = null; renderLab(); return; }
    if (act === 'cmd-run') { runCommand(); return; }
    if (act === 'cmd-clear') { state.cmdOut = null; renderLab(); return; }
    if (act === 'wan-up') { runWan('up'); return; }
    if (act === 'wan-renew') { runWan('renew'); return; }
    if (act === 'wan-down') {
      arm(btn, '断开 WAN', () => runWan('down'));
      return;
    }
    if (act === 'reload') {
      arm(btn, '重载', async () => {
        try {
          await state.api.ReloadNetwork();
          toast('网络重载指令已下发', 'ok', 2800);
          setTimeout(() => refreshSnapshot(false), 2200);
        } catch (e) { toastError(e); }
      });
      return;
    }
    if (act === 'reboot') {
      arm(btn, '重启', async () => {
        try {
          await state.api.RebootRouter();
          toast('重启指令已发送，设备恢复后请重新连接', 'info', 6000);
          leaveToConnect('设备正在重启。请等待约 60 秒后重新连接。');
        } catch (e) { toastError(e); }
      });
      return;
    }
    if (act === 'disconnect') {
      try { await state.api.Disconnect(); } catch (e) {}
      leaveToConnect('');
      toast('SSH 会话已断开', 'info', 2200);
    }
  }

  async function runWan(action) {
    try {
      await state.api.WanAction(action);
      const text = action === 'up' ? '已请求拉起 WAN 接口。'
        : action === 'renew' ? '已请求续约 WAN 租约。' : '已断开 WAN 接口。';
      setOpMsg('link', 'ok', text);
      renderLab();
      setTimeout(() => { refreshSnapshot(false); if (state.labOpen) refreshLab(); }, 1600);
    } catch (e) { opFail('link', e); }
  }

  async function runProbe(kind) {
    const input = $('#probeTarget');
    const target = (input ? input.value : state.probeTarget).trim();
    if (!target) { toast('请输入探测目标', 'error'); return; }
    state.probeTarget = target;
    state.probeKind = kind;
    state.probeBusy = true;
    renderLab();
    try {
      const r = await state.api.RunProbe(kind, target);
      state.probeOut = {
        command: r.command || (kind + ' ' + target),
        output: r.output || '',
        exitCode: r.exitCode,
        durationMs: r.durationMs,
      };
    } catch (e) {
      state.probeOut = { command: kind + ' ' + target, output: errText(e), exitCode: -1, durationMs: 0 };
    }
    state.probeBusy = false;
    renderLab();
    scrollLab('acc-probe');
  }

  function probeSummary(kind, r) {
    if (!r) return null;
    const out = String(r.output || '');
    const code = Number(r.exitCode);
    if (kind === 'ping') {
      const loss = /(\d+(?:\.\d+)?)%\s*packet loss/i.exec(out);
      const avg = /=\s*[\d.]+\/([\d.]+)\//.exec(out);
      const reachable = code === 0 && (!loss || Number(loss[1]) < 100);
      if (reachable) {
        return { tone: 'ok', text: '网络可达' + (avg ? '，平均延迟 ' + avg[1] + ' ms' : '') + (loss ? '，丢包 ' + loss[1] + '%' : '') };
      }
      return { tone: 'bad', text: '目标不可达' + (loss ? '，丢包 ' + loss[1] + '%' : '') };
    }
    const hops = out.split('\n').filter((l) => /^\s*\d+\s+\S/.test(l)).length;
    if (code === 0 || hops > 0) return { tone: 'ok', text: '路由追踪完成，共 ' + hops + ' 跳' };
    return { tone: 'bad', text: '路由追踪没有返回结果' };
  }

  function cmdVerdict(r) {
    if (!r) return null;
    const code = Number(r.exitCode);
    if (code === 0) return { tone: 'ok', text: '执行成功，退出码 0' };
    if (code < 0) return { tone: 'bad', text: '命令没有执行完成，展开输出查看原因' };
    return { tone: 'warn', text: '命令返回非零退出码 ' + code };
  }

  async function runCommand() {
    const input = $('#sshInput');
    const cmd = (input ? input.value : state.cmdLast).trim();
    if (!cmd) { toast('请输入命令', 'error'); return; }
    state.cmdLast = cmd;
    state.cmdHistory.push(cmd);
    if (state.cmdHistory.length > 50) state.cmdHistory.shift();
    state.cmdIdx = state.cmdHistory.length;
    state.cmdBusy = true;
    renderLab();
    try {
      const r = await state.api.ExecCommand(cmd);
      state.cmdOut = { command: r.command || cmd, output: r.output || '', exitCode: r.exitCode, durationMs: r.durationMs };
    } catch (e) {
      state.cmdOut = { command: cmd, output: errText(e), exitCode: -1, durationMs: 0 };
    }
    state.cmdBusy = false;
    renderLab();
    scrollLab('acc-ssh');
  }

  /* ================= leave / confirm ================= */
  function leaveToConnect(message) {
    state.leaving = true;
    state.connected = false;
    stopPolling();
    closeLab();
    $('#cellView').classList.add('hidden');
    $('#cellView').setAttribute('aria-hidden', 'true');
    state.cellLoading = false;
    state.cellError = '';
    state.snap = null;
    state.lab = null;
    state.console = null;
    state.consoleError = '';
    state.consoleLoading = false;
    state.cfgVals = null;
    state.cfgList = [];
    state.logOut = '';
    state.info = null;
    state.expandedMac = '';
    state.tileSig = '';
    state.clientSig = '';
    state.pinned = state.pinned;
    const topo = $('#topoWrap');
    if (topo) { topo.dataset.sig = ''; topo.innerHTML = ''; }
    $('#mainView').classList.add('hidden');
    $('#connectView').classList.remove('hidden');
    $('#connDot').className = 'status-dot off';
    state.linkDown = true;
    showConnectError(message || '');
    updateReconnectUI();
    $('#resetFpBtn').classList.toggle('hidden', !(state.cfg && (state.cfg.fingerprint || '').length));
  }

  function arm(btn, label, fn) {
    if (btn.dataset.armed === '1') {
      clearInterval(btn._armTimer);
      btn.dataset.armed = '';
      btn.classList.remove('armed');
      btn.textContent = label;
      fn();
      return;
    }
    btn.dataset.armed = '1';
    btn.classList.add('armed');
    let left = 3;
    btn.textContent = '确认 ' + left;
    btn._armTimer = setInterval(() => {
      left -= 1;
      if (left <= 0) {
        clearInterval(btn._armTimer);
        btn.dataset.armed = '';
        btn.classList.remove('armed');
        btn.textContent = label;
        return;
      }
      btn.textContent = '确认 ' + left;
    }, 1000);
  }

  /* ================= events ================= */
  function bindEvents() {
    $$('#themeBtn, #connectThemeBtn').forEach((b) => b.addEventListener('click', toggleTheme));
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden && state.connected) startPolling();
    });
    document.addEventListener('click', (e) => {
      const b = e.target.closest('[data-win]');
      if (!b || !state.api) return;
      const act = b.dataset.win;
      if (act === 'min') state.api.WindowMinimise();
      else if (act === 'max') {
        state.api.WindowToggleMaximise();
        setTimeout(syncWinMax, 80);
      } else if (act === 'close') state.api.CloseApp();
    });

    document.querySelectorAll('.topbar, .dragbar').forEach((el) => {
      el.addEventListener('dblclick', (e) => {
        if (e.target.closest('button, input, select, textarea, a')) return;
        if (!state.api || !state.api.WindowToggleMaximise) return;
        state.api.WindowToggleMaximise();
        setTimeout(syncWinMax, 80);
      });
    });

    async function syncWinMax() {
      if (!state.api || !state.api.WindowIsMaximised) return;
      try {
        document.body.classList.toggle('win-max', !!await state.api.WindowIsMaximised());
      } catch (e) {}
    }
    window.addEventListener('resize', syncWinMax);
    syncWinMax();
    $('#pinBtn').addEventListener('click', async () => {
      state.pinned = !state.pinned;
      try {
        await state.api.SetAlwaysOnTop(state.pinned);
        const u = $('#pinBtn use');
        u.setAttribute('href', state.pinned ? '#i-pin' : '#i-pin-off');
        $('#pinBtn').classList.toggle('active', state.pinned);
        toast(state.pinned ? '窗口已置顶' : '已取消置顶', 'info', 1600);
      } catch (e) { toastError(e); }
    });
    $('#proBtn').addEventListener('click', () => state.labOpen ? closeLab() : openLab());
    const reconnectMainBtn = $('#reconnectBtn');
    if (reconnectMainBtn) reconnectMainBtn.addEventListener('click', () => reconnectLast());
    const reconnectConnectBtn = $('#connectReconnectBtn');
    if (reconnectConnectBtn) reconnectConnectBtn.addEventListener('click', () => reconnectLast());
    $('#labBack').addEventListener('click', closeLab);
    $('#labRefreshBtn').addEventListener('click', () => refreshLab());
    $$('.lab-tabs button[data-labtab]').forEach((b) => b.addEventListener('click', () => {
      if (state.labTab === b.dataset.labtab) return;
      state.labTab = b.dataset.labtab;
      renderLab();
      if (state.labTab === 'tools') fetchLab();
      else if (state.labTab === 'market') { ensureMarketRepos(); ensureMarketIndex(); loadMarketPolicy(); }
      else fetchConsole();
    }));
    const offlineMarketBtn = $('#offlineMarketBtn');
    if (offlineMarketBtn) offlineMarketBtn.addEventListener('click', () => openLab(null, 'market'));
    $('#pluginChips').addEventListener('click', () => { state.cTab = 'plugins'; openLab(null, 'console'); });
    $('#topoWrap').addEventListener('click', (e) => {
      const port = e.target.closest('[data-port]');
      if (!port) return;
      if (port.dataset.port === 'cell') { openCellView(); return; }
      state.cTab = port.dataset.port === 'wifi' ? 'wireless' : 'network';
      openLab(null, 'console');
    });
    $('#cellBack').addEventListener('click', closeCellView);
    $('#cellRefresh').addEventListener('click', () => loadCellData(true));
    $('#labDisconnectBtn').addEventListener('click', () => {
      if (!state.api) return;
      state.api.Disconnect().catch(() => {});
      leaveToConnect('');
      toast('SSH 会话已断开', 'info', 2200);
    });

    $('#connectForm').addEventListener('submit', (e) => { e.preventDefault(); doConnect(); });
    $('#authSeg').addEventListener('click', (e) => {
      const b = e.target.closest('button[data-auth]');
      if (b) setAuth(b.dataset.auth);
    });
    $('#pickKeyBtn').addEventListener('click', async () => {
      if (!state.api) return;
      try {
        const p = await state.api.PickKeyFile();
        if (p) $('#keyPathInput').value = p;
      } catch (e) { toastError(e); }
    });
    $('#resetFpBtn').addEventListener('click', async () => {
      if (!state.api) return;
      try {
        await state.api.ResetFingerprint();
        if (state.cfg) state.cfg.fingerprint = '';
        $('#resetFpBtn').classList.add('hidden');
        showConnectError('已清除保存的主机指纹，下次连接将重新信任当前设备。');
      } catch (e) { toastError(e); }
    });

    $('#copyIPBtn').addEventListener('click', () => {
      const ip = state.snap && state.snap.wan && state.snap.wan.ip;
      copyText(ip || '', '公网 IP');
    });
    $('#maskIPBtn').addEventListener('click', () => {
      state.ipMasked = !state.ipMasked;
      $('#maskIPBtn use').setAttribute('href', state.ipMasked ? '#i-eye-off' : '#i-eye');
      if (state.snap) renderSnapshot(state.snap);
    });

    $('#tilesGrid').addEventListener('click', (e) => {
      const b = e.target.closest('[data-tile]');
      if (b) handleTile(b);
    });

    $('#clientList').addEventListener('click', (e) => {
      const actBtn = e.target.closest('[data-cact]');
      if (actBtn) { handleClientAction(actBtn); return; }
      const main = e.target.closest('[data-client]');
      if (main) toggleClient(main.dataset.client);
    });

    $('#clientList').addEventListener('input', (e) => {
      if (e.target.type === 'range') {
        const val = e.target.parentElement.querySelector('.val');
        if (val) val.textContent = e.target.value + 'M';
      }
    });

    $('#labContent').addEventListener('click', (e) => {
      const ctab = e.target.closest('[data-ctab]');
      if (ctab) { setConsoleTab(ctab.dataset.ctab); return; }
      const accHead = e.target.closest('[data-acc]');
      if (accHead) {
        const id = accHead.dataset.acc;
        if (state.accOpen.has(id)) state.accOpen.delete(id); else state.accOpen.add(id);
        renderLab();
        return;
      }
      const target = e.target.closest('[data-target]');
      if (target) {
        state.probeTarget = target.dataset.target;
        $$('#labContent [data-target]').forEach((c) => c.classList.toggle('active', c === target));
        const input = $('#probeTarget');
        if (input) input.value = state.probeTarget;
        return;
      }
      const actBtn = e.target.closest('[data-act]');
      if (actBtn) handleLabAction(actBtn);
    });

    $('#labContent').addEventListener('toggle', (e) => {
      const d = e.target && e.target.closest ? e.target.closest('details[data-mkdetail]') : null;
      if (!d) return;
      if (d.dataset.mkdetail === 'repo') state.market.repoOpen = d.open;
    }, true);

    $('#labContent').addEventListener('change', (e) => {
      if (e.target.id === 'cfgSelect') loadConfigValues(e.target.value);
      if (e.target.id === 'logKind') state.logKind = e.target.value;
      if (e.target.id === 'mkCat') {
        state.market.cat = e.target.value;
        state.market.page = 0;
        updateMarketList();
      }
      if (e.target.id === 'mkOnlyNew') {
        state.market.onlyNew = !!e.target.checked;
        state.market.page = 0;
        updateMarketList();
      }
      if (e.target.id === 'mkSort') {
        state.market.sort = e.target.value;
        updateMarketList();
      }
      if (e.target.id === 'mkConfirm') {
        state.market.confirmed = !!e.target.checked;
        renderMarket();
      }
      if (e.target.id === 'mkDangerInstall') {
        state.market.allowDangerous = !!e.target.checked;
        syncMarketInstall();
      }
      if (e.target.id === 'mkConfirmA') {
        state.market.confirmA = !!e.target.checked;
        syncMarketInstall();
      }
      if (e.target.id === 'mkConfirmB') {
        state.market.confirmB = !!e.target.checked;
        syncMarketInstall();
      }
    });

    // 子标签横向滚动：鼠标滚轮 / 触控板纵向滚动都映射到横向。
    $('#labContent').addEventListener('wheel', (e) => {
      const tabs = e.target.closest('.ctabs');
      if (!tabs) return;
      if (Math.abs(e.deltaY) <= Math.abs(e.deltaX)) return;
      if (tabs.scrollWidth <= tabs.clientWidth) return;
      tabs.scrollLeft += e.deltaY;
      e.preventDefault();
    }, { passive: false });
    window.addEventListener('resize', () => {
      if (state.labTab === 'console') syncCtabs();
    });

    $('#labContent').addEventListener('input', (e) => {
      if (e.target.id === 'mkConfirmHost') {
        state.market.confirmHost = e.target.value.trim();
        syncMarketInstall();
        return;
      }
      if (e.target.id === 'mkRepos') {
        state.market.reposText = e.target.value;
        state.market.reposTouched = true;
        return;
      }
      if (e.target.id === 'mkSearch') {
        state.market.search = e.target.value;
        state.market.page = 0;
        updateMarketList();
        return;
      }
      if (e.target.id !== 'pkgFilter') return;
      state.pkgFilter = e.target.value;
      const f = state.pkgFilter.toLowerCase();
      $$('#labContent .pkg-row').forEach((row) => {
        row.style.display = !f || (row.dataset.pkg || '').indexOf(f) >= 0 ? '' : 'none';
      });
    });

    document.addEventListener('keydown', (e) => {
      const mod = e.ctrlKey || e.metaKey;
      if (mod && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        if (state.connected) { state.labOpen ? closeLab() : openLab(); }
        return;
      }
      if (e.key === 'Escape' && state.labOpen) { e.preventDefault(); closeLab(); return; }
      if (e.target && e.target.id === 'sshInput' && e.key === 'Enter') {
        e.preventDefault();
        runCommand();
        return;
      }
      if (e.target && e.target.id === 'sshInput' && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
        if (!state.cmdHistory.length) return;
        e.preventDefault();
        if (e.key === 'ArrowUp') state.cmdIdx = Math.max(0, state.cmdIdx - 1);
        else state.cmdIdx = Math.min(state.cmdHistory.length, state.cmdIdx + 1);
        const v = state.cmdIdx >= state.cmdHistory.length ? '' : state.cmdHistory[state.cmdIdx];
        e.target.value = v;
        return;
      }
      if (e.target && e.target.id === 'probeTarget' && e.key === 'Enter') {
        e.preventDefault();
        runProbe('ping');
      }
    });
  }

  /* ================= init ================= */
  async function init() {
    if (MOCK) {
      if (new URLSearchParams(location.search).get('preview') !== '0') {
        document.documentElement.classList.add('preview');
      }
    }
    let theme = 'dark';
    try { theme = localStorage.getItem('prolite-theme') || 'dark'; } catch (e) {}
    applyTheme(theme);

    state.api = MOCK ? mockApi() : makeWailsApi();
    bindEvents();

    if (MOCK && NO_AUTOCONNECT) {
      fillConfig(await state.api.GetConfig());
      return;
    }

    if (!state.api) {
      fillConfig({ host: '192.168.100.1', port: 22, user: 'root', auth: 'none', saved: false, platform: 'browser', version: '0.1.0' });
      showConnectError('未检测到 Pro-Lite 运行时。请启动桌面程序，或在浏览器中使用 ?mock=1 预览。');
      return;
    }

    try {
      const cfg = await state.api.GetConfig();
      state.cfg = cfg;
      fillConfig(cfg);
      const u = $('#pinBtn use');
      u.setAttribute('href', state.pinned ? '#i-pin' : '#i-pin-off');
      $('#pinBtn').classList.toggle('active', state.pinned);
      if (MOCK) {
        const info = await state.api.Connect({ host: cfg.host, port: cfg.port, user: cfg.user, auth: 'agent', remember: true });
        enterMain(info);
        if (new URLSearchParams(location.search).get('cell') === '1') setTimeout(openCellView, 700);
        const ctab = new URLSearchParams(location.search).get('console');
        if (ctab) setTimeout(() => { state.cTab = ctab; openLab(null, 'console'); }, 700);
        const acc = new URLSearchParams(location.search).get('acc');
        if (acc) acc.split(',').forEach((k) => { if (k.trim()) state.accOpen.add(k.trim()); });
        const labtab = new URLSearchParams(location.search).get('lab');
        const focus = new URLSearchParams(location.search).get('focus');
        if (labtab) setTimeout(() => openLab(focus ? 'acc-' + focus : null, labtab), 700);
        if (new URLSearchParams(location.search).get('market') === '1') {
          setTimeout(() => openLab(null, 'market'), 700);
        }
      } else if (cfg.saved && cfg.auth !== 'password') {
        doConnect();
      }
    } catch (e) {
      showConnectError(errText(e));
    }
  }

  init();
})();
