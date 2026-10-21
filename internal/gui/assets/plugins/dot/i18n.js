(() => {
  const keys = ["Pair using the private local file in the terminal, then refresh status. Never paste credentials here.","Disable Dot locally","Look up original request","Requests from this window","Task capability verified","Task probe required in DotControl","Dot request recovered","Cancellation is intent only; refresh to inspect completion"];
  const words = {
    zh: ["在终端中使用本机私密配对文件配对，然后刷新状态。请勿在此粘贴凭据。","在本机停用 Dot","查询原请求","此窗口的请求","任务能力已验证","需在 DotControl 中验证任务能力","已恢复 Dot 请求","取消仅表示意图；请刷新以查看完成状态"],
    "zh-TW": ["在終端機中使用本機私密配對檔案配對，然後重新整理狀態。請勿在此貼上憑證。","在本機停用 Dot","查詢原請求","此視窗的請求","工作能力已驗證","需在 DotControl 中驗證工作能力","已復原 Dot 請求","取消僅表示意圖；請重新整理以查看完成狀態"],
    ja: ["端末でローカルの非公開ペアリングファイルを使用し、状態を更新してください。認証情報をここに貼り付けないでください。","この端末で Dot を無効化","元のリクエストを照会","このウィンドウのリクエスト","タスク機能を検証済み","DotControl でタスク機能の検証が必要","Dot リクエストを復元しました","キャンセルは要求のみです。更新して完了状態を確認してください"],
    de: ["Mit der privaten lokalen Kopplungsdatei im Terminal koppeln und den Status aktualisieren. Hier keine Zugangsdaten einfügen.","Dot lokal deaktivieren","Ursprüngliche Anfrage abfragen","Anfragen aus diesem Fenster","Aufgabenfunktion bestätigt","Aufgabenprüfung in DotControl erforderlich","Dot Anfrage wiederhergestellt","Abbruch ist nur angefordert; zum Prüfen des Abschlusses aktualisieren"]
  };
  const dictionaries = Object.fromEntries(Object.entries(words).map(([lang, values]) => [lang, Object.fromEntries(keys.map((key, i) => [key.includes("_") || /^[a-z]+$/.test(key) ? "Dot state: " + key : key, values[i]]))]));
  const cancel = {zh:"请求取消", "zh-TW":"請求取消", ja:"キャンセルを要求", de:"Abbruch anfordern"};
  for (const lang of Object.keys(dictionaries)) dictionaries[lang]["Request cancellation"] = cancel[lang];
  const acceptance = {zh:"本机接受记录", "zh-TW":"本機接受記錄", ja:"ローカル受付記録", de:"Lokaler Annahmeeintrag"};
  for (const lang of Object.keys(dictionaries)) dictionaries[lang]["Local acceptance record"] = acceptance[lang];
  const capacity = {zh:"Dot 容量：已保留 {retained} / {limit}；剩余 {remaining}。验证任务也计入容量。", "zh-TW":"Dot 容量：已保留 {retained} / {limit}；剩餘 {remaining}。驗證工作也計入容量。", ja:"Dot 容量：{limit} 件中 {retained} 件を保持、残り {remaining} 件。検証タスクも含みます。", de:"Dot Kapazität: {retained} von {limit} gespeichert; {remaining} frei. Prüfaufgaben zählen ebenfalls."};
  for (const lang of Object.keys(dictionaries)) dictionaries[lang]["Dot capacity: {retained} retained of {limit}; {remaining} remaining. Probes also count."] = capacity[lang];
  magpie.strings(dictionaries);
})();
