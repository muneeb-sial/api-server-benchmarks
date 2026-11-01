import 'reflect-metadata';
import { NestFactory } from '@nestjs/core';
import { FastifyAdapter, type NestFastifyApplication } from '@nestjs/platform-fastify';
import { AppModule } from './app.module.js';

const app = await NestFactory.create<NestFastifyApplication>(AppModule, new FastifyAdapter(), {
  logger: ['error', 'warn'],
});
await app.listen(Number(process.env.PORT ?? 3000), '127.0.0.1');
