import 'reflect-metadata';
import { NestFactory } from '@nestjs/core';
import { NodeAdapter, type NestNodeApplication } from '@nestjs/platform-node';
import { AppModule } from './app.module.js';

const app = await NestFactory.create<NestNodeApplication>(AppModule, new NodeAdapter(), {
  logger: ['error', 'warn'],
});
await app.listen(Number(process.env.PORT ?? 3000), '127.0.0.1');
