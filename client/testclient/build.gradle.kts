// Test-Client: spielt ein Gerät gegenüber dem Server (Google-Login, Geräte-
// schlüssel, Schlüssel-Backup, Datensicherung). Nur zum Ausprobieren, nicht Teil
// der App.
//
//   ./gradlew :testclient:run --args="anmelden"
plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.kotlin.serialization)
    application
}

dependencies {
    implementation(project(":verbindung"))
    implementation(libs.coroutines)
    implementation(libs.serialization.json)

    testImplementation(platform(libs.junit.bom))
    testImplementation(libs.junit.jupiter)
    testRuntimeOnly(libs.junit.launcher)
}

application {
    mainClass = "com.avlreminder.testclient.MainKt"
    applicationName = "testclient"
}

// Eingaben (Wiederherstellungsschlüssel) kommen über die Konsole.
tasks.named<JavaExec>("run") {
    standardInput = System.`in`
}
