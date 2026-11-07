# Linux image of the server for `bench run -docker`; same JVM flags as bench.json.
FROM maven:3.9-eclipse-temurin-17 AS build
WORKDIR /app
COPY pom.xml ./
RUN mvn -q dependency:go-offline
COPY src src
RUN mvn -q -DskipTests package

FROM eclipse-temurin:17-jre
WORKDIR /app
COPY --from=build /app/target/app.jar app.jar
CMD exec java -XX:MaxRAM=${MEMORY_MB}m -XX:MaxRAMPercentage=75 -XX:ActiveProcessorCount=${WORKERS} -jar app.jar
