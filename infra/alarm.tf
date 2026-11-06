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
# Funktion. Kommt fast einen Tag lang keiner an (Budget, Thema, Trigger oder
# Eventarc fehlen), schützt der Kill-Switch nicht mehr – und die Regel oben sähe
# nichts, weil dann auch nichts geloggt wird. Kommt der Aufruf an, scheitert aber
# (Funktion kaputt, Rechte entzogen), loggt Cloud Run die Antwort als WARNING
# oder ERROR, und die Regel oben schlägt an.
#
# Grenze: Die Bedingung schlägt erst an, nachdem es einmal einen Aufruf gab.
# Nach einem Neuaufbau deshalb einmal eine Meldung unter der Schwelle schicken
# (docs/stand.md, „Befehle“) und prüfen, dass sie ankommt.
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

# Alarm bei ungewöhnlich viel ausgehendem Datenverkehr des Servers (Entscheidung
# nach Review-Lauf 8, Frage 3).
#
# Die Tageslimits gelten je Konto (Sicherung holen höchstens 50 × 8 MiB, also
# etwa 400 MiB am Tag). Wer viele Google-Konten anlegt, vervielfacht das; das
# Budget meldet sich erst bei 25 % (5 €). Diese Regel meldet sich früher: wenn
# der Server in 23 Stunden mehr als 1 GiB sendet. Im Normalbetrieb sind es
# wenige MiB (Sitzungen, gelegentlich eine Sicherung beim Einrichten).
#
# Gezählt wird alles, was der Server sendet (Metrik sent_bytes_count, alle Werte
# des Labels kind): Antworten an Apps, aber auch Uploads nach Cloud Storage. Das
# ist vorsichtig – lieber eine Mail zu viel. Antworten über Googles Frontend
# könnten als kind="google" statt "internet" gezählt werden; ein Filter auf
# "internet" könnte sie deshalb übersehen.
#
# Fenster 23 Stunden: Google empfiehlt höchstens 24 Stunden minus die
# Verzögerung der Metrik (bis 120 s). Ein gleitendes Fenster fängt sowohl einen
# schnellen Abfluss (Alarm nach wenigen Minuten) als auch einen langsamen.
#
# Kosten: eine Bedingung mit einer Zeitreihe, laut Preisbeispielen von Google
# ca. 0,35 $ je Bedingung plus ca. 0,04 $ für die abgefragten Punkte im Monat.
resource "google_monitoring_alert_policy" "egress" {
  display_name = "Server sendet ungewöhnlich viele Daten"
  combiner     = "OR"

  conditions {
    display_name = "Mehr als 1 GiB gesendet in 23 Stunden"
    condition_threshold {
      filter = join(" AND ", [
        "metric.type=\"run.googleapis.com/container/network/sent_bytes_count\"",
        "resource.type=\"cloud_run_revision\"",
        "resource.labels.service_name=\"${google_cloud_run_v2_service.server.name}\"",
      ])
      comparison      = "COMPARISON_GT"
      threshold_value = 1073741824 # 1 GiB in Bytes
      duration        = "0s"

      # Summe über alle Revisionen und Werte von kind: Jeder Deploy beginnt eine
      # neue Zeitreihe; ohne Summe verteilte sich der Verkehr auf mehrere.
      aggregations {
        alignment_period     = "82800s"
        per_series_aligner   = "ALIGN_SUM"
        cross_series_reducer = "REDUCE_SUM"
        group_by_fields      = ["resource.label.service_name"]
      }

      trigger {
        count = 1
      }
    }
  }

  alert_strategy {
    auto_close = "86400s"
  }

  documentation {
    mime_type = "text/markdown"
    content   = <<-EOT
      Der Server hat in 23 Stunden mehr als 1 GiB gesendet. Normal sind wenige MiB.

      - Wer holt? Logs „Sicherung geholt“ je Gerät: `gcloud logging read 'resource.labels.service_name="server" AND jsonPayload.message="Sicherung geholt"' --bucket=logs-eu --location=${var.region} --view=_AllLogs --limit=50`
      - Viele neue Konten oder Geräte? Logs „Gerät angemeldet“ mit `neues_konto=true`.
      - Notfalls den Dienst sperren wie der Kill-Switch (`allUsers` entfernen) und die Ursache klären.
    EOT
  }

  notification_channels = [google_monitoring_notification_channel.email.id]
}
