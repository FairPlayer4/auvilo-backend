// Test-Client: spielt ein Gerät gegenüber dem Server (Google-Login, Geräte-
// schlüssel, Schlüssel-Backup). Nur zum Ausprobieren, nicht Teil der App.
//
//   ./gradlew :testclient:run --args="anmelden"
plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.kotlin.serialization)
    alias(libs.plugins.protobuf)
    application
}

dependencies {
    implementation(project(":protokoll"))
    implementation(libs.connect)
    implementation(libs.connect.okhttp)
    implementation(libs.connect.javalite)
    implementation(libs.okhttp)
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

// Code für die Server-Schnittstelle (proto/avlreminder/v1): Nachrichten (lite)
// und Connect-Clients. Die Client-Formate (inhalt) kommen aus :protokoll.
sourceSets {
    main {
        proto {
            srcDir("../../proto")
            include("avlreminder/v1/**")
        }
    }
}

// Der Connect-Generator ist ein Java-Programm (ein JAR), kein natives Werkzeug je
// Plattform. Über "artifact" würde das Protobuf-Plugin eine Plattform-Variante
// suchen, die es nicht gibt; deshalb holen wir das JAR selbst und geben den Pfad an.
val connectGenerator: Configuration by configurations.creating { isTransitive = false }
dependencies { connectGenerator(libs.connect.codegen) }

protobuf {
    protoc { artifact = libs.protoc.get().toString() }
    plugins {
        create("connect-kotlin") { path = connectGenerator.singleFile.absolutePath }
    }
    generateProtoTasks {
        all().configureEach {
            builtins {
                named("java") { option("lite") }
            }
            plugins {
                create("connect-kotlin")
            }
        }
    }
}
