-- MariaDB dump
--
-- Host: localhost    Database: outrodb
-- ------------------------------------------------------

/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET NAMES utf8mb4 */;

--
-- Table structure for table `vazia`
--

DROP TABLE IF EXISTS `vazia`;
CREATE TABLE `vazia` (
  `id` int NOT NULL AUTO_INCREMENT,
  `label` text NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
