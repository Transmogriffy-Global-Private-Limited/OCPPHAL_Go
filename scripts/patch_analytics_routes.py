from pathlib import Path
p = Path("internal/httpapi/server.go")
s = p.read_text(encoding="utf-8")
nl = "\r\n" if "\r\n" in s else "\n"
if "case \"/api/charger_analytics\"" not in s:
    api_start = s.find("func (s *Server) api(")
    if api_start < 0:
        raise SystemExit("api function not found")
    default_marker = "\tdefault:" + nl
    default_index = s.find(default_marker, api_start)
    if default_index < 0:
        raise SystemExit("default case not found after api function")
    insert = "\tcase \"/api/charger_analytics\":" + nl + "\t\ts.chargerAnalytics(w, r)" + nl + "\tcase \"/api/check_charger_inactivity\":" + nl + "\t\ts.checkChargerInactivity(w, r)" + nl
    s = s[:default_index] + insert + s[default_index:]
p.write_text(s, encoding="utf-8")
