// Protokoll-Modul: nur Tink, Protobuf (lite) und die Kotlin-Standardbibliothek.
// Kein Netz, kein Android – damit es später unverändert in die App passt.
plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.kotlin.serialization) // nur für die Testvektoren (JSON)
    alias(libs.plugins.protobuf)
    `java-library`
}

dependencies {
    api(libs.tink)
    api(libs.protobuf.javalite)

    testImplementation(platform(libs.junit.bom))
    testImplementation(libs.junit.jupiter)
    testImplementation(libs.serialization.json)
    testImplementation(libs.coroutines) // runBlocking für die Einrichtung (suspend)
    testRuntimeOnly(libs.junit.launcher)
}

// Code aus den Formaten, die nur Clients lesen (proto/avlreminder/inhalt).
sourceSets {
    main {
        proto {
            srcDir("../../proto")
            include("avlreminder/inhalt/**")
        }
    }
}

protobuf {
    protoc { artifact = libs.protoc.get().toString() }
    generateProtoTasks {
        all().configureEach {
            builtins {
                named("java") { option("lite") }
            }
        }
    }
}

// Testvektoren, die auch der Go-Server prüft (proto/testvektoren).
tasks.test {
    val testvektoren = rootDir.resolve("../proto/testvektoren")
    systemProperty("testvektoren", testvektoren.absolutePath)
    // Nur zum einmaligen Erzeugen neuer Vektoren (InhaltTestvektorenTest).
    systemProperty("testvektoren.erzeugen", providers.gradleProperty("testvektorenErzeugen").getOrElse("nein"))
    // Damit Gradle den Test neu ausführt, wenn sich nur die Vektoren ändern.
    inputs.dir(testvektoren).withPathSensitivity(PathSensitivity.RELATIVE)
}
