import 'reflect-metadata';
import { NestFactory } from '@nestjs/core';
import { ExpressAdapter, type NestExpressApplication } from '@nestjs/platform-express';
import { AppModule } from './app.module.js';

const app = await NestFactory.create<NestExpressApplication>(AppModule, new ExpressAdapter(), {
  logger: ['error', 'warn'],
});
await app.listen(Number(process.env.PORT ?? 3000), process.env.HOST ?? '127.0.0.1');
