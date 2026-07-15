// pingping i18n —— 六语言字典。语言偏好存 localStorage,首次按浏览器语言猜。
const I18N = {
  zh: {
    tagline: '链路质量示波器 · 监控告诉你挂没挂,pingping 告诉你活得好不好',
    tabScope: '烟雾图', tabHosts: '主机', tabHooks: '通知',
    push: '推送实时报告到飞书', pushed: '报告已推送至全部 webhook',
    lossLbl: '丢包 %', burstLbl: '突发·窗口内', filterAll: '全部',
    legend: '亮线 = 每轮中位数 · 烟雾 = 全部样本 · 红柱 = 丢包% · ◆ = 突发',
    concOK: '🟢 {name}:近 24h P99 {p99} ms · 丢包 {loss}% · 突发 {bursts} 次',
    concAlert: '🔴 {name}:{kind} —— 处理中',
    thType: '类型', thName: '名称', thHost: '地址', thInterval: '间隔',
    thPace: '节奏', thSens: '敏感度', thStatus: '状态',
    stOK: '正常', stAlert: '告警中', stObserve: '仅观测',
    hkName: '名称', hkURL: 'Webhook 地址(已脱敏)', hkKinds: '接收消息', hkSecret: '签名',
    hkAll: '全收', hkYes: '✓ 已配置', hkNo: '–',
    loginTitle: '登录 pingping', username: '用户名', password: '密码',
    signin: '登 录', language: '语言', loginFailed: '用户名或密码错误', logout: '退出',
  },
  en: {
    tagline: 'Link quality oscilloscope · Monitoring tells you if it is down, pingping tells you how well it lives',
    tabScope: 'Smoke', tabHosts: 'Hosts', tabHooks: 'Notify',
    push: 'Push report to Feishu', pushed: 'Report pushed to all webhooks',
    lossLbl: 'Loss %', burstLbl: 'Bursts · window', filterAll: 'All',
    legend: 'Line = per-round median · Smoke = all samples · Red bars = loss% · ◆ = burst',
    concOK: '🟢 {name}: 24h P99 {p99} ms · loss {loss}% · {bursts} bursts',
    concAlert: '🔴 {name}: {kind} — active',
    thType: 'Type', thName: 'Name', thHost: 'Address', thInterval: 'Interval',
    thPace: 'Pace', thSens: 'Sensitivity', thStatus: 'Status',
    stOK: 'OK', stAlert: 'Alerting', stObserve: 'Observe-only',
    hkName: 'Name', hkURL: 'Webhook URL (masked)', hkKinds: 'Kinds', hkSecret: 'Signature',
    hkAll: 'all', hkYes: '✓ configured', hkNo: '–',
    loginTitle: 'Sign in to pingping', username: 'Username', password: 'Password',
    signin: 'Sign in', language: 'Language', loginFailed: 'Wrong username or password', logout: 'Logout',
  },
  ja: {
    tagline: 'リンク品質オシロスコープ · 死活は監視が、品質は pingping が教える',
    tabScope: 'スモーク', tabHosts: 'ホスト', tabHooks: '通知',
    push: 'レポートを Feishu へ送信', pushed: '全 webhook へ送信しました',
    lossLbl: '損失 %', burstLbl: 'バースト·窓内', filterAll: 'すべて',
    legend: '輝線 = 中央値 · 煙 = 全サンプル · 赤棒 = 損失% · ◆ = バースト',
    concOK: '🟢 {name}:24h P99 {p99} ms · 損失 {loss}% · バースト {bursts} 回',
    concAlert: '🔴 {name}:{kind} —— 対応中',
    thType: '種別', thName: '名称', thHost: 'アドレス', thInterval: '間隔',
    thPace: 'ペース', thSens: '感度', thStatus: '状態',
    stOK: '正常', stAlert: '警報中', stObserve: '観測のみ',
    hkName: '名称', hkURL: 'Webhook URL(マスク済)', hkKinds: '受信種別', hkSecret: '署名',
    hkAll: 'すべて', hkYes: '✓ 設定済', hkNo: '–',
    loginTitle: 'pingping にログイン', username: 'ユーザー名', password: 'パスワード',
    signin: 'ログイン', language: '言語', loginFailed: 'ユーザー名またはパスワードが違います', logout: 'ログアウト',
  },
  ko: {
    tagline: '링크 품질 오실로스코프 · 다운 여부는 모니터링이, 품질은 pingping이 알려줍니다',
    tabScope: '스모크', tabHosts: '호스트', tabHooks: '알림',
    push: 'Feishu로 보고서 전송', pushed: '모든 webhook으로 전송했습니다',
    lossLbl: '손실 %', burstLbl: '버스트·창 내', filterAll: '전체',
    legend: '선 = 라운드 중앙값 · 연기 = 전체 샘플 · 빨간 막대 = 손실% · ◆ = 버스트',
    concOK: '🟢 {name}: 24h P99 {p99} ms · 손실 {loss}% · 버스트 {bursts}회',
    concAlert: '🔴 {name}: {kind} — 대응 중',
    thType: '유형', thName: '이름', thHost: '주소', thInterval: '간격',
    thPace: '페이스', thSens: '민감도', thStatus: '상태',
    stOK: '정상', stAlert: '경보 중', stObserve: '관찰만',
    hkName: '이름', hkURL: 'Webhook 주소(마스킹)', hkKinds: '수신 종류', hkSecret: '서명',
    hkAll: '전체', hkYes: '✓ 설정됨', hkNo: '–',
    loginTitle: 'pingping 로그인', username: '사용자 이름', password: '비밀번호',
    signin: '로그인', language: '언어', loginFailed: '사용자 이름 또는 비밀번호가 올바르지 않습니다', logout: '로그아웃',
  },
  es: {
    tagline: 'Osciloscopio de calidad de enlace · El monitoreo dice si está caído, pingping dice qué tan bien vive',
    tabScope: 'Humo', tabHosts: 'Equipos', tabHooks: 'Avisos',
    push: 'Enviar informe a Feishu', pushed: 'Informe enviado a todos los webhooks',
    lossLbl: 'Pérdida %', burstLbl: 'Ráfagas · ventana', filterAll: 'Todos',
    legend: 'Línea = mediana por ronda · Humo = todas las muestras · Barras rojas = pérdida% · ◆ = ráfaga',
    concOK: '🟢 {name}: P99 24h {p99} ms · pérdida {loss}% · {bursts} ráfagas',
    concAlert: '🔴 {name}: {kind} — activo',
    thType: 'Tipo', thName: 'Nombre', thHost: 'Dirección', thInterval: 'Intervalo',
    thPace: 'Ritmo', thSens: 'Sensibilidad', thStatus: 'Estado',
    stOK: 'OK', stAlert: 'En alerta', stObserve: 'Solo observar',
    hkName: 'Nombre', hkURL: 'URL del webhook (oculta)', hkKinds: 'Tipos', hkSecret: 'Firma',
    hkAll: 'todos', hkYes: '✓ configurada', hkNo: '–',
    loginTitle: 'Iniciar sesión en pingping', username: 'Usuario', password: 'Contraseña',
    signin: 'Entrar', language: 'Idioma', loginFailed: 'Usuario o contraseña incorrectos', logout: 'Salir',
  },
  de: {
    tagline: 'Link-Qualitäts-Oszilloskop · Monitoring sagt, ob es down ist — pingping, wie gut es lebt',
    tabScope: 'Rauch', tabHosts: 'Hosts', tabHooks: 'Meldungen',
    push: 'Bericht an Feishu senden', pushed: 'Bericht an alle Webhooks gesendet',
    lossLbl: 'Verlust %', burstLbl: 'Bursts · Fenster', filterAll: 'Alle',
    legend: 'Linie = Median je Runde · Rauch = alle Proben · Rote Balken = Verlust% · ◆ = Burst',
    concOK: '🟢 {name}: 24h P99 {p99} ms · Verlust {loss}% · {bursts} Bursts',
    concAlert: '🔴 {name}: {kind} — aktiv',
    thType: 'Typ', thName: 'Name', thHost: 'Adresse', thInterval: 'Intervall',
    thPace: 'Tempo', thSens: 'Empfindlichkeit', thStatus: 'Status',
    stOK: 'OK', stAlert: 'Alarm', stObserve: 'Nur beobachten',
    hkName: 'Name', hkURL: 'Webhook-URL (maskiert)', hkKinds: 'Arten', hkSecret: 'Signatur',
    hkAll: 'alle', hkYes: '✓ konfiguriert', hkNo: '–',
    loginTitle: 'Bei pingping anmelden', username: 'Benutzername', password: 'Passwort',
    signin: 'Anmelden', language: 'Sprache', loginFailed: 'Benutzername oder Passwort falsch', logout: 'Abmelden',
  },
};

const LANGS = [['zh', '中文'], ['en', 'English'], ['ja', '日本語'], ['ko', '한국어'], ['es', 'Español'], ['de', 'Deutsch']];

function curLang() {
  const saved = localStorage.getItem('pp_lang');
  if (I18N[saved]) return saved;
  const nav = (navigator.language || 'zh').slice(0, 2);
  return I18N[nav] ? nav : 'en';
}

function setLang(l) { localStorage.setItem('pp_lang', l); }

function T(key, vars) {
  let s = I18N[curLang()][key] ?? I18N.zh[key] ?? key;
  if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll('{' + k + '}', v);
  return s;
}

// 填充语言下拉框并绑定切换
function mountLangSelect(sel, onChange) {
  sel.innerHTML = '';
  LANGS.forEach(([code, label]) => {
    const o = document.createElement('option');
    o.value = code; o.textContent = label;
    if (code === curLang()) o.selected = true;
    sel.appendChild(o);
  });
  sel.onchange = () => { setLang(sel.value); onChange && onChange(); };
}
