package main

const helpText = `WavPTT - riproduttore di messaggi WAV con PTT su porta seriale
per Windows, Mac e Linux

COME FUNZIONA
Quando premi una memoria (bottoni F1...F8 o tasti F1...F8), il programma:
  1. apre la porta scelta nel menu PTT (oppure si collega alla radio
     tramite TCI, vedi sotto);
  2. porta allo stato alto (attivo) la linea scelta, RTS oppure DTR;
     l'altra linea resta bassa;
  3. attende il Ritardo (ms) impostato (da 0 a 300 ms);
  4. riproduce il file WAV della memoria sulla scheda audio TX MIXER;
  5. a fine file abbassa la linea e chiude la porta.

TASTI
  F1...F8                memoria, con la finestra di WavPTT attiva
  Ctrl+Alt+F1...F8       memoria, anche quando lavori in altri programmi
                         (su Mac: Ctrl+Option+F1...F8)
  Esc                    interrompe la trasmissione e rilascia il PTT
                         (con la finestra di WavPTT attiva)

Se all'avvio anche una sola combinazione Ctrl+Alt+F1...F8 e' gia' usata
da un altro programma, compare un avviso rosso lampeggiante e tutti i
comandi Ctrl+Alt restano disattivati fino alla chiusura.
Mac: i tasti globali richiedono il permesso in Impostazioni di Sistema >
Privacy e sicurezza > Accessibilita'. Se i tasti F1...F8 comandano
luminosita' e volume, premili insieme al tasto fn.
Linux: i tasti globali funzionano nelle sessioni X11 (non Wayland).

PIU' MEMORIE IN FILA
Se premi altre memorie mentre una sta suonando, vengono messe in coda
nell'ordine in cui le premi. Finita una, il programma attende 50 ms e
fa partire la successiva, senza rilasciare il PTT. Il PTT si rilascia
solo alla fine dell'ultima memoria in coda.

ASSEGNARE I FILE WAV
Il bottone "File WAV..." apre la finestra delle memorie:
  ...      sceglie il file WAV per quella memoria
  Play     ascolto di prova sull'uscita audio predefinita del computer,
           senza attivare il PTT (con filtro, compressore e volume)
  Stop     ferma l'ascolto di prova
  Salva    copia i file scelti nella sottocartella WAV, accanto al
           programma, e salva le memorie
  Annulla  chiude senza salvare

AUDIO
Windows: la scheda audio viene usata tramite il driver MME.
Mac: Core Audio. Linux: ALSA / PulseAudio / PipeWire.
La scheda viene impegnata sempre a 8000 campioni al secondo (16 bit).
I file WAV con frequenza diversa vengono convertiti a 8000 Hz prima di
partire; la conversione taglia le frequenze sopra circa 3,8 kHz.

FILTRO (menu a tendina accanto al cursore del volume)
  Nessun filtro   audio senza filtro passa banda
  300-2700 Hz     passa banda per la SSB "normale"
  500-2400 Hz     passa banda piu' stretto, voce piu' penetrante
  500-2100 Hz     passa banda ancora piu' stretto, per segnali deboli
I filtri sono Butterworth del 4 ordine su entrambi i lati.

COMPRESSORE (menu a tendina: off oppure da 3 a 13 dB, a scatti di 1 dB)
Con N dB scelti, i picchi piu' forti del messaggio vengono ridotti di N dB
(rapporto 2:1, soglia 2N dB sotto il picco, attacco 5 ms, rilascio
150 ms) e poi si recuperano N dB: i picchi restano allo stesso livello,
le parti deboli salgono fino a N dB.

SCHEMA DI LAVORAZIONE
Conversione a 8000 Hz, compressore, filtro, amplificazione (VOLUME),
limitazione dei picchi.

IMPOSTAZIONI
  TX MIXER      scheda audio di uscita verso il trasmettitore
  VOLUME        livello del file WAV verso la scheda audio. Il cursore
                ha andamento logaritmico e a ogni avvio parte dal
                massimo: a sinistra (0) silenzio, poco dopo il livello
                originale, a destra il massimo (circa 1000 volte).
                Vale anche per il Play di prova. Il nuovo livello si
                applica dalla memoria successiva; se il segnale
                distorce (suono "rotto"), abbassalo.
  Filtro        passa banda sull'audio emesso
  Compressore   compressione sull'audio emesso: off oppure 3...13 dB
  PTT           porta seriale che comanda il PTT
                (Windows: COM1...; Mac: /dev/cu.*; Linux: /dev/ttyUSB0...)
  RTS/DTR/TCI   linea della porta usata per il PTT, oppure TCI
  Ritardo (ms)  attesa tra PTT attivo e inizio dell'audio (0-300)

I menu TX MIXER e PTT si aggiornano ogni volta che li apri, quindi
un'interfaccia USB collegata dopo l'avvio compare nell'elenco.
Linux: per usare la porta seriale l'utente deve far parte del gruppo
"dialout" (sudo usermod -aG dialout $USER, poi rientrare).

PTT E AUDIO TRAMITE TCI (SunSDR / ExpertSDR e programmi compatibili)
Scegli TCI accanto a RTS e DTR: al posto della porta seriale compare
l'indirizzo del server TCI, nella forma host:porta.
  ExpertSDR (SunSDR)         127.0.0.1:40001  (valore iniziale)
  AetherSDR, Thetis e altri  di solito porta 50001
Se la radio e' su un altro computer, scrivi il suo indirizzo di rete
(per esempio 192.168.1.20:40001).
Con TCI il programma:
  1. si collega alla radio e attiva la trasmissione con "trx" e
     sorgente audio TCI;
  2. attende il Ritardo (ms) mandando silenzio;
  3. manda il file WAV alla radio attraverso il TCI, lavorato come
     sempre (8000 Hz, compressore, filtro, volume);
  4. a fine audio rilascia la trasmissione e chiude il collegamento.
Con TCI la scheda audio TX MIXER non serve e resta disattivata.
In ExpertSDR il TCI deve essere abilitato (Opzioni > TCI) e deve
poter comandare la trasmissione. Se la radio non risponde, il motivo
compare nella riga di stato e la trasmissione non parte.

COLORI
Il menu Colori, accanto al bottone Help, sceglie lo schema: Chiaro,
Notte, Nero, Blu, Viola, Verde. La scelta si applica subito ed e'
salvata in WavPTT.ini.

DOVE SONO I DATI
Tutto resta nella cartella del programma (su Mac: nella cartella che
contiene WavPTT.app):
  WavPTT.ini   impostazioni e memorie
  WAV/         copie dei file WAV assegnati
Il programma non scrive nulla in AppData o nelle cartelle di sistema.
La cartella deve essere scrivibile: su Mac conviene tenere WavPTT.app
in una cartella propria (per esempio Documenti/WavPTT), non in
Applicazioni.

NOTE
- Formati accettati: WAV PCM (8/16/24/32 bit) e float, mono o stereo.
- Se la porta e' occupata da un altro programma, il PTT non si attiva
  e l'audio non parte: lo vedi scritto nella riga di stato.
`
