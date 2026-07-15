// pingping i18n —— 15 语言字典,与 README 表里如一。偏好存 localStorage,首次按浏览器语言猜。
const I18N = {
  zh: {
    tagline: '链路质量示波器 · 监控告诉你挂没挂,pingping 告诉你活得好不好',
    tabScope: '烟雾图', tabHosts: '主机', tabHooks: '通知',
    push: '推送实时报告', pushed: '报告已推送至全部 webhook',
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
  zht: {
    tagline: '鏈路品質示波器 · 監控告訴你掛沒掛,pingping 告訴你活得好不好',
    tabScope: '煙霧圖', tabHosts: '主機', tabHooks: '通知',
    push: '推送即時報告', pushed: '報告已推送至全部 webhook',
    lossLbl: '丟包 %', burstLbl: '突發·視窗內', filterAll: '全部',
    legend: '亮線 = 每輪中位數 · 煙霧 = 全部樣本 · 紅柱 = 丟包% · ◆ = 突發',
    concOK: '🟢 {name}:近 24h P99 {p99} ms · 丟包 {loss}% · 突發 {bursts} 次',
    concAlert: '🔴 {name}:{kind} —— 處理中',
    thType: '類型', thName: '名稱', thHost: '位址', thInterval: '間隔',
    thPace: '節奏', thSens: '敏感度', thStatus: '狀態',
    stOK: '正常', stAlert: '告警中', stObserve: '僅觀測',
    hkName: '名稱', hkURL: 'Webhook 位址(已脫敏)', hkKinds: '接收訊息', hkSecret: '簽名',
    hkAll: '全收', hkYes: '✓ 已設定', hkNo: '–',
    loginTitle: '登入 pingping', username: '使用者名稱', password: '密碼',
    signin: '登 入', language: '語言', loginFailed: '使用者名稱或密碼錯誤', logout: '登出',
  },
  en: {
    tagline: 'Link quality oscilloscope · Monitoring tells you if it is down, pingping tells you how well it lives',
    tabScope: 'Smoke', tabHosts: 'Hosts', tabHooks: 'Notify',
    push: 'Push live report', pushed: 'Report pushed to all webhooks',
    lossLbl: 'Loss %', burstLbl: 'Bursts · window', filterAll: 'All',
    legend: 'Line = per-round median · Smoke = all samples · Red bars = loss% · ◆ = burst',
    concOK: '🟢 {name}: 24h P99 {p99} ms · loss {loss}% · {bursts} bursts',
    concAlert: '🔴 {name}: {kind} — active',
    thType: 'Type', thName: 'Name', thHost: 'Address', thInterval: 'Interval',
    thPace: 'Pace', thSens: 'Sensitivity', thStatus: 'Status',
    stOK: 'OK', stAlert: 'Alerting', stObserve: 'Observe-only',
    hkName: 'Name', hkURL: 'Webhook URL (masked)', hkKinds: 'Kinds', hkSecret: 'Signature',
    hkAll: 'All', hkYes: '✓ Set', hkNo: '–',
    loginTitle: 'Sign in to pingping', username: 'Username', password: 'Password',
    signin: 'Sign in', language: 'Language', loginFailed: 'Wrong username or password', logout: 'Sign out',
  },
  ja: {
    tagline: 'リンク品質オシロスコープ · 死活は監視が、体調は pingping が教える',
    tabScope: 'スモーク', tabHosts: 'ホスト', tabHooks: '通知',
    push: 'レポートを送信', pushed: '全 webhook に送信しました',
    lossLbl: '損失 %', burstLbl: 'バースト·期間内', filterAll: 'すべて',
    legend: '輝線 = 各ラウンド中央値 · 煙 = 全サンプル · 赤棒 = 損失% · ◆ = バースト',
    concOK: '🟢 {name}:24h P99 {p99} ms · 損失 {loss}% · バースト {bursts} 回',
    concAlert: '🔴 {name}:{kind} —— 対応中',
    thType: '種別', thName: '名称', thHost: 'アドレス', thInterval: '間隔',
    thPace: 'ペース', thSens: '感度', thStatus: '状態',
    stOK: '正常', stAlert: '警報中', stObserve: '観測のみ',
    hkName: '名称', hkURL: 'Webhook アドレス(マスク済)', hkKinds: '受信対象', hkSecret: '署名',
    hkAll: 'すべて', hkYes: '✓ 設定済', hkNo: '–',
    loginTitle: 'pingping にログイン', username: 'ユーザー名', password: 'パスワード',
    signin: 'ログイン', language: '言語', loginFailed: 'ユーザー名またはパスワードが違います', logout: 'ログアウト',
  },
  ko: {
    tagline: '링크 품질 오실로스코프 · 다운 여부는 모니터링이, 상태는 pingping이 알려줍니다',
    tabScope: '스모크', tabHosts: '호스트', tabHooks: '알림',
    push: '실시간 보고서 전송', pushed: '모든 webhook에 전송됨',
    lossLbl: '손실 %', burstLbl: '버스트·기간 내', filterAll: '전체',
    legend: '밝은 선 = 라운드 중앙값 · 연기 = 전체 샘플 · 빨간 막대 = 손실% · ◆ = 버스트',
    concOK: '🟢 {name}: 24h P99 {p99} ms · 손실 {loss}% · 버스트 {bursts}회',
    concAlert: '🔴 {name}: {kind} — 대응 중',
    thType: '유형', thName: '이름', thHost: '주소', thInterval: '간격',
    thPace: '페이스', thSens: '민감도', thStatus: '상태',
    stOK: '정상', stAlert: '경보 중', stObserve: '관측 전용',
    hkName: '이름', hkURL: 'Webhook 주소(마스킹됨)', hkKinds: '수신 종류', hkSecret: '서명',
    hkAll: '전체', hkYes: '✓ 설정됨', hkNo: '–',
    loginTitle: 'pingping 로그인', username: '사용자 이름', password: '비밀번호',
    signin: '로그인', language: '언어', loginFailed: '사용자 이름 또는 비밀번호가 올바르지 않습니다', logout: '로그아웃',
  },
  de: {
    tagline: 'Link-Qualitäts-Oszilloskop · Monitoring sagt ob es down ist, pingping sagt wie gut es lebt',
    tabScope: 'Rauch', tabHosts: 'Hosts', tabHooks: 'Benachrichtigung',
    push: 'Bericht senden', pushed: 'Bericht an alle Webhooks gesendet',
    lossLbl: 'Verlust %', burstLbl: 'Bursts · Fenster', filterAll: 'Alle',
    legend: 'Linie = Median je Runde · Rauch = alle Messwerte · Rote Balken = Verlust% · ◆ = Burst',
    concOK: '🟢 {name}: 24h P99 {p99} ms · Verlust {loss}% · {bursts} Bursts',
    concAlert: '🔴 {name}: {kind} — aktiv',
    thType: 'Typ', thName: 'Name', thHost: 'Adresse', thInterval: 'Intervall',
    thPace: 'Takt', thSens: 'Empfindlichkeit', thStatus: 'Status',
    stOK: 'OK', stAlert: 'Alarm', stObserve: 'Nur Beobachtung',
    hkName: 'Name', hkURL: 'Webhook-URL (maskiert)', hkKinds: 'Nachrichten', hkSecret: 'Signatur',
    hkAll: 'Alle', hkYes: '✓ Gesetzt', hkNo: '–',
    loginTitle: 'Bei pingping anmelden', username: 'Benutzername', password: 'Passwort',
    signin: 'Anmelden', language: 'Sprache', loginFailed: 'Benutzername oder Passwort falsch', logout: 'Abmelden',
  },
  fr: {
    tagline: 'Oscilloscope de qualité de lien · La supervision dit si c\'est tombé, pingping dit comment ça vit',
    tabScope: 'Fumée', tabHosts: 'Hôtes', tabHooks: 'Notifications',
    push: 'Envoyer le rapport', pushed: 'Rapport envoyé à tous les webhooks',
    lossLbl: 'Perte %', burstLbl: 'Rafales · fenêtre', filterAll: 'Tous',
    legend: 'Ligne = médiane par tour · Fumée = tous les échantillons · Barres rouges = perte% · ◆ = rafale',
    concOK: '🟢 {name} : P99 24h {p99} ms · perte {loss}% · {bursts} rafales',
    concAlert: '🔴 {name} : {kind} — en cours',
    thType: 'Type', thName: 'Nom', thHost: 'Adresse', thInterval: 'Intervalle',
    thPace: 'Cadence', thSens: 'Sensibilité', thStatus: 'État',
    stOK: 'OK', stAlert: 'Alerte', stObserve: 'Observation seule',
    hkName: 'Nom', hkURL: 'URL du webhook (masquée)', hkKinds: 'Messages', hkSecret: 'Signature',
    hkAll: 'Tous', hkYes: '✓ Définie', hkNo: '–',
    loginTitle: 'Connexion à pingping', username: 'Nom d\'utilisateur', password: 'Mot de passe',
    signin: 'Se connecter', language: 'Langue', loginFailed: 'Identifiants incorrects', logout: 'Déconnexion',
  },
  es: {
    tagline: 'Osciloscopio de calidad de enlace · El monitoreo dice si está caído, pingping dice cómo vive',
    tabScope: 'Humo', tabHosts: 'Hosts', tabHooks: 'Avisos',
    push: 'Enviar informe', pushed: 'Informe enviado a todos los webhooks',
    lossLbl: 'Pérdida %', burstLbl: 'Ráfagas · ventana', filterAll: 'Todos',
    legend: 'Línea = mediana por ronda · Humo = todas las muestras · Barras rojas = pérdida% · ◆ = ráfaga',
    concOK: '🟢 {name}: P99 24h {p99} ms · pérdida {loss}% · {bursts} ráfagas',
    concAlert: '🔴 {name}: {kind} — activo',
    thType: 'Tipo', thName: 'Nombre', thHost: 'Dirección', thInterval: 'Intervalo',
    thPace: 'Ritmo', thSens: 'Sensibilidad', thStatus: 'Estado',
    stOK: 'OK', stAlert: 'Alertando', stObserve: 'Solo observación',
    hkName: 'Nombre', hkURL: 'URL del webhook (enmascarada)', hkKinds: 'Mensajes', hkSecret: 'Firma',
    hkAll: 'Todos', hkYes: '✓ Configurada', hkNo: '–',
    loginTitle: 'Iniciar sesión en pingping', username: 'Usuario', password: 'Contraseña',
    signin: 'Entrar', language: 'Idioma', loginFailed: 'Usuario o contraseña incorrectos', logout: 'Salir',
  },
  pt: {
    tagline: 'Osciloscópio de qualidade de link · O monitoramento diz se caiu, o pingping diz como vive',
    tabScope: 'Fumaça', tabHosts: 'Hosts', tabHooks: 'Avisos',
    push: 'Enviar relatório', pushed: 'Relatório enviado a todos os webhooks',
    lossLbl: 'Perda %', burstLbl: 'Rajadas · janela', filterAll: 'Todos',
    legend: 'Linha = mediana por rodada · Fumaça = todas as amostras · Barras vermelhas = perda% · ◆ = rajada',
    concOK: '🟢 {name}: P99 24h {p99} ms · perda {loss}% · {bursts} rajadas',
    concAlert: '🔴 {name}: {kind} — ativo',
    thType: 'Tipo', thName: 'Nome', thHost: 'Endereço', thInterval: 'Intervalo',
    thPace: 'Ritmo', thSens: 'Sensibilidade', thStatus: 'Estado',
    stOK: 'OK', stAlert: 'Alertando', stObserve: 'Só observação',
    hkName: 'Nome', hkURL: 'URL do webhook (mascarada)', hkKinds: 'Mensagens', hkSecret: 'Assinatura',
    hkAll: 'Todos', hkYes: '✓ Definida', hkNo: '–',
    loginTitle: 'Entrar no pingping', username: 'Usuário', password: 'Senha',
    signin: 'Entrar', language: 'Idioma', loginFailed: 'Usuário ou senha incorretos', logout: 'Sair',
  },
  it: {
    tagline: 'Oscilloscopio di qualità del collegamento · Il monitoraggio dice se è giù, pingping dice come vive',
    tabScope: 'Fumo', tabHosts: 'Host', tabHooks: 'Notifiche',
    push: 'Invia rapporto', pushed: 'Rapporto inviato a tutti i webhook',
    lossLbl: 'Perdita %', burstLbl: 'Raffiche · finestra', filterAll: 'Tutti',
    legend: 'Linea = mediana per giro · Fumo = tutti i campioni · Barre rosse = perdita% · ◆ = raffica',
    concOK: '🟢 {name}: P99 24h {p99} ms · perdita {loss}% · {bursts} raffiche',
    concAlert: '🔴 {name}: {kind} — attivo',
    thType: 'Tipo', thName: 'Nome', thHost: 'Indirizzo', thInterval: 'Intervallo',
    thPace: 'Ritmo', thSens: 'Sensibilità', thStatus: 'Stato',
    stOK: 'OK', stAlert: 'In allarme', stObserve: 'Solo osservazione',
    hkName: 'Nome', hkURL: 'URL webhook (mascherato)', hkKinds: 'Messaggi', hkSecret: 'Firma',
    hkAll: 'Tutti', hkYes: '✓ Impostata', hkNo: '–',
    loginTitle: 'Accedi a pingping', username: 'Nome utente', password: 'Password',
    signin: 'Accedi', language: 'Lingua', loginFailed: 'Nome utente o password errati', logout: 'Esci',
  },
  ru: {
    tagline: 'Осциллограф качества канала · Мониторинг скажет, упал ли он, pingping — как он живёт',
    tabScope: 'Дым', tabHosts: 'Хосты', tabHooks: 'Уведомления',
    push: 'Отправить отчёт', pushed: 'Отчёт отправлен на все webhook',
    lossLbl: 'Потери %', burstLbl: 'Всплески · окно', filterAll: 'Все',
    legend: 'Линия = медиана раунда · Дым = все замеры · Красные столбцы = потери% · ◆ = всплеск',
    concOK: '🟢 {name}: P99 за 24ч {p99} мс · потери {loss}% · всплесков {bursts}',
    concAlert: '🔴 {name}: {kind} — активно',
    thType: 'Тип', thName: 'Имя', thHost: 'Адрес', thInterval: 'Интервал',
    thPace: 'Темп', thSens: 'Чувствительность', thStatus: 'Статус',
    stOK: 'OK', stAlert: 'Тревога', stObserve: 'Только наблюдение',
    hkName: 'Имя', hkURL: 'URL webhook (замаскирован)', hkKinds: 'Сообщения', hkSecret: 'Подпись',
    hkAll: 'Все', hkYes: '✓ Задана', hkNo: '–',
    loginTitle: 'Вход в pingping', username: 'Имя пользователя', password: 'Пароль',
    signin: 'Войти', language: 'Язык', loginFailed: 'Неверное имя пользователя или пароль', logout: 'Выйти',
  },
  th: {
    tagline: 'ออสซิลโลสโคปคุณภาพลิงก์ · มอนิเตอร์บอกว่าล่มไหม pingping บอกว่าเป็นอย่างไร',
    tabScope: 'ควัน', tabHosts: 'โฮสต์', tabHooks: 'การแจ้งเตือน',
    push: 'ส่งรายงานสด', pushed: 'ส่งรายงานไปยัง webhook ทั้งหมดแล้ว',
    lossLbl: 'สูญหาย %', burstLbl: 'เบิร์สต์·ในหน้าต่าง', filterAll: 'ทั้งหมด',
    legend: 'เส้นสว่าง = ค่ามัธยฐานต่อรอบ · ควัน = ตัวอย่างทั้งหมด · แท่งแดง = สูญหาย% · ◆ = เบิร์สต์',
    concOK: '🟢 {name}: P99 24ชม. {p99} ms · สูญหาย {loss}% · เบิร์สต์ {bursts} ครั้ง',
    concAlert: '🔴 {name}: {kind} — กำลังเกิด',
    thType: 'ชนิด', thName: 'ชื่อ', thHost: 'ที่อยู่', thInterval: 'ช่วงเวลา',
    thPace: 'จังหวะ', thSens: 'ความไว', thStatus: 'สถานะ',
    stOK: 'ปกติ', stAlert: 'กำลังแจ้งเตือน', stObserve: 'สังเกตเท่านั้น',
    hkName: 'ชื่อ', hkURL: 'ที่อยู่ Webhook (ปิดบังแล้ว)', hkKinds: 'ข้อความ', hkSecret: 'ลายเซ็น',
    hkAll: 'ทั้งหมด', hkYes: '✓ ตั้งค่าแล้ว', hkNo: '–',
    loginTitle: 'เข้าสู่ระบบ pingping', username: 'ชื่อผู้ใช้', password: 'รหัสผ่าน',
    signin: 'เข้าสู่ระบบ', language: 'ภาษา', loginFailed: 'ชื่อผู้ใช้หรือรหัสผ่านไม่ถูกต้อง', logout: 'ออกจากระบบ',
  },
  id: {
    tagline: 'Osiloskop kualitas tautan · Monitoring memberi tahu jika mati, pingping memberi tahu seberapa sehat',
    tabScope: 'Asap', tabHosts: 'Host', tabHooks: 'Notifikasi',
    push: 'Kirim laporan', pushed: 'Laporan terkirim ke semua webhook',
    lossLbl: 'Loss %', burstLbl: 'Burst · jendela', filterAll: 'Semua',
    legend: 'Garis = median per putaran · Asap = semua sampel · Batang merah = loss% · ◆ = burst',
    concOK: '🟢 {name}: P99 24j {p99} ms · loss {loss}% · {bursts} burst',
    concAlert: '🔴 {name}: {kind} — aktif',
    thType: 'Tipe', thName: 'Nama', thHost: 'Alamat', thInterval: 'Interval',
    thPace: 'Tempo', thSens: 'Sensitivitas', thStatus: 'Status',
    stOK: 'OK', stAlert: 'Peringatan', stObserve: 'Hanya pantau',
    hkName: 'Nama', hkURL: 'URL webhook (disamarkan)', hkKinds: 'Pesan', hkSecret: 'Tanda tangan',
    hkAll: 'Semua', hkYes: '✓ Diatur', hkNo: '–',
    loginTitle: 'Masuk ke pingping', username: 'Nama pengguna', password: 'Kata sandi',
    signin: 'Masuk', language: 'Bahasa', loginFailed: 'Nama pengguna atau kata sandi salah', logout: 'Keluar',
  },
  vi: {
    tagline: 'Máy hiện sóng chất lượng liên kết · Giám sát cho biết có sập không, pingping cho biết sống ra sao',
    tabScope: 'Khói', tabHosts: 'Máy chủ', tabHooks: 'Thông báo',
    push: 'Gửi báo cáo', pushed: 'Đã gửi báo cáo tới tất cả webhook',
    lossLbl: 'Mất gói %', burstLbl: 'Bùng phát · cửa sổ', filterAll: 'Tất cả',
    legend: 'Đường sáng = trung vị mỗi vòng · Khói = toàn bộ mẫu · Cột đỏ = mất gói% · ◆ = bùng phát',
    concOK: '🟢 {name}: P99 24h {p99} ms · mất gói {loss}% · {bursts} lần bùng phát',
    concAlert: '🔴 {name}: {kind} — đang xảy ra',
    thType: 'Loại', thName: 'Tên', thHost: 'Địa chỉ', thInterval: 'Chu kỳ',
    thPace: 'Nhịp', thSens: 'Độ nhạy', thStatus: 'Trạng thái',
    stOK: 'Bình thường', stAlert: 'Đang cảnh báo', stObserve: 'Chỉ quan sát',
    hkName: 'Tên', hkURL: 'URL webhook (đã che)', hkKinds: 'Tin nhắn', hkSecret: 'Chữ ký',
    hkAll: 'Tất cả', hkYes: '✓ Đã đặt', hkNo: '–',
    loginTitle: 'Đăng nhập pingping', username: 'Tên đăng nhập', password: 'Mật khẩu',
    signin: 'Đăng nhập', language: 'Ngôn ngữ', loginFailed: 'Tên đăng nhập hoặc mật khẩu sai', logout: 'Đăng xuất',
  },
  ar: {
    tagline: 'راسم إشارة لجودة الوصلة · المراقبة تخبرك إن سقطت، وpingping يخبرك كيف تعيش',
    tabScope: 'الدخان', tabHosts: 'المضيفون', tabHooks: 'الإشعارات',
    push: 'إرسال التقرير', pushed: 'تم إرسال التقرير إلى جميع الـ webhook',
    lossLbl: 'الفقد %', burstLbl: 'اندفاعات · النافذة', filterAll: 'الكل',
    legend: 'الخط = الوسيط لكل جولة · الدخان = كل العينات · الأعمدة الحمراء = الفقد% · ◆ = اندفاع',
    concOK: '🟢 {name}: P99 خلال 24س {p99} ms · فقد {loss}% · {bursts} اندفاعات',
    concAlert: '🔴 {name}: {kind} — جارٍ',
    thType: 'النوع', thName: 'الاسم', thHost: 'العنوان', thInterval: 'الفاصل',
    thPace: 'الإيقاع', thSens: 'الحساسية', thStatus: 'الحالة',
    stOK: 'سليم', stAlert: 'إنذار', stObserve: 'مراقبة فقط',
    hkName: 'الاسم', hkURL: 'عنوان Webhook (مموّه)', hkKinds: 'الرسائل', hkSecret: 'التوقيع',
    hkAll: 'الكل', hkYes: '✓ مضبوط', hkNo: '–',
    loginTitle: 'تسجيل الدخول إلى pingping', username: 'اسم المستخدم', password: 'كلمة المرور',
    signin: 'دخول', language: 'اللغة', loginFailed: 'اسم المستخدم أو كلمة المرور غير صحيحة', logout: 'خروج',
  },
};

const LANGS = [
  ['zh', '简体中文'], ['zht', '繁體中文'], ['en', 'English'], ['ja', '日本語'],
  ['ko', '한국어'], ['de', 'Deutsch'], ['fr', 'Français'], ['es', 'Español'],
  ['pt', 'Português'], ['it', 'Italiano'], ['ru', 'Русский'], ['th', 'ไทย'],
  ['id', 'Bahasa Indonesia'], ['vi', 'Tiếng Việt'], ['ar', 'العربية'],
];

function curLang() {
  const saved = localStorage.getItem('pp_lang');
  if (I18N[saved]) return saved;
  const nav = (navigator.language || 'zh');
  if (/^zh-(TW|HK|MO)/i.test(nav)) return 'zht';
  const two = nav.slice(0, 2);
  return I18N[two] ? two : 'en';
}

function setLang(l) { localStorage.setItem('pp_lang', l); applyDir(); }

// 阿拉伯语整页 RTL,其余 LTR
function applyDir() {
  document.documentElement.dir = curLang() === 'ar' ? 'rtl' : 'ltr';
  document.documentElement.lang = curLang();
}
applyDir();

function T(key, vars) {
  let s = I18N[curLang()][key] ?? I18N.en[key] ?? I18N.zh[key] ?? key;
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
