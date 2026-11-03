# Alarm, wenn der Kill-Switch eingreift oder scheitert (Review Lauf 1, E9).
#
# Kosten gehen vor Verfügbarkeit – ein stiller Ausfall des Kill-Switch darf
# nicht unbemerkt bleiben, und ein Eingriff soll sofort auffallen. Die Funktion
# loggt jeden Eingriff (neu gesperrte Dienste, Trennen) als WARNING und jeden
# Fehler als ERROR (funktion.go); ist schon alles gesperrt, nur INFO. Bei WARNING
# und ERROR schickt diese Regel eine E-Mail.
#
# Kosten: eine Bedingung, laut Google ca. 0,10 $ pro Monat (Log-Alarme zahlen
# nur die Bedingung, keine Zeitreihen).

resource "google_monitoring_notification_channel" "email" {
  display_name = "E-Mail an Kevin"
  type         = "email"
  labels = {
    email_address = var.alarm_email
  }
  depends_on = [google_project_service.apis]
}

resource "google_monitoring_alert_policy" "killswitch" {
  display_name = "Kill-Switch hat eingegriffen oder ist gescheitert"
  combiner     = "OR"

  conditions {
    display_name = "Kill-Switch-Log mit WARNING oder schlimmer"
    condition_matched_log {
      filter = join(" AND ", [
        "resource.type=\"cloud_run_revision\"",
        "resource.labels.service_name=\"${google_cloudfunctions2_function.killswitch.name}\"",
        "severity>=WARNING",
      ])
    }
  }

  alert_strategy {
    # Höchstens eine Mail alle 5 Minuten; der Vorfall schließt sich nach 30 Minuten
    # ohne neue Treffer von selbst.
    notification_rate_limit {
      period = "300s"
    }
    auto_close = "1800s"
  }

  documentation {
    mime_type = "text/markdown"
    content   = <<-EOT
      Der Kill-Switch hat eingegriffen (Budget überschritten) oder ist gescheitert.

      - Logs: `gcloud logging read 'resource.labels.service_name="killswitch"' --bucket=logs-eu --location=${var.region} --view=_AllLogs --limit=10`
      - Gesperrte Dienste bleiben gesperrt, bis sie bewusst von Hand geöffnet werden.
      - Ursache klären: Fehler im Code oder Angriff?
    EOT
  }

  notification_channels = [google_monitoring_notification_channel.email.id]
}
