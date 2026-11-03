# Alarm, wenn der Kill-Switch eingreift oder scheitert (Review Lauf 1, E9).
#
# Kosten gehen vor Verfügbarkeit – ein stiller Ausfall des Kill-Switch darf
# nicht unbemerkt bleiben (Regel "killswitch_still" unten), und ein Eingriff
# soll sofort auffallen. Die Funktion
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

# Alarm, wenn der Kill-Switch gar nicht mehr läuft (Review Lauf 5, E3).
#
# Das Budget meldet sich mehrmals täglich; jede Meldung ist ein Aufruf der
# Funktion. Kommt fast einen Tag lang keiner an (Funktion kaputt deployt,
# Trigger oder Thema weg, Rechte entzogen), schützt der Kill-Switch nicht mehr –
# und die Regel oben sähe nichts, weil dann auch nichts geloggt wird.
#
# Kosten: eine weitere Bedingung, ca. 0,10 $ pro Monat; die Metrik
# run.googleapis.com/request_count liefert Cloud Run kostenlos.
resource "google_monitoring_alert_policy" "killswitch_still" {
  display_name = "Kill-Switch wird nicht mehr aufgerufen"
  combiner     = "OR"

  conditions {
    display_name = "Kein Aufruf der Funktion seit 23,5 Stunden"
    condition_absent {
      filter = join(" AND ", [
        "metric.type=\"run.googleapis.com/request_count\"",
        "resource.type=\"cloud_run_revision\"",
        "resource.labels.service_name=\"${google_cloudfunctions2_function.killswitch.name}\"",
      ])
      # Längste Dauer, die Cloud Monitoring zulässt.
      duration = "84600s"

      # Summe über alle Revisionen: Jeder Deploy der Funktion beginnt eine neue
      # Zeitreihe; ohne Summe meldete die alte Revision nach dem Deploy "still".
      aggregations {
        alignment_period     = "3600s"
        per_series_aligner   = "ALIGN_SUM"
        cross_series_reducer = "REDUCE_SUM"
        group_by_fields      = ["resource.label.service_name"]
      }

      trigger {
        count = 1
      }
    }
  }

  documentation {
    mime_type = "text/markdown"
    content   = <<-EOT
      Der Kill-Switch wurde seit fast einem Tag nicht aufgerufen. Solange das so
      ist, sperrt nichts die Dienste, wenn das Budget überschritten wird.

      - Läuft die Funktion? `gcloud functions describe killswitch --region=${var.region}`
      - Kommen Budget-Meldungen an? Thema `budget-meldungen`, Budget in der Konsole (Abrechnung → Budgets).
      - Logs: `gcloud logging read 'resource.labels.service_name="killswitch"' --bucket=logs-eu --location=${var.region} --view=_AllLogs --limit=10`
    EOT
  }

  notification_channels = [google_monitoring_notification_channel.email.id]
}
