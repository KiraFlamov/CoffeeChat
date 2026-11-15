/**
 * Точка входа Web Worker. Весь код, который работает с ключами, выполняется здесь,
 * а не в основном потоке страницы.
 */
import { expose } from 'comlink';
import { cryptoApi } from './api';

expose(cryptoApi);
