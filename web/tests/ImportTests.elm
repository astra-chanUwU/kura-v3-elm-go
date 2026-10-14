module ImportTests exposing (allPassed, suite)

import Api.Import as ImportApi
import Domain.Post as Post
import Feature.Import.State as ImportState
import Json.Decode as Decode


access : ImportState.Access
access =
    { writesEnabled = True, accessGeneration = 1 }


lockedAccess : ImportState.Access
lockedAccess =
    { writesEnabled = False, accessGeneration = 1 }


hashA : String
hashA =
    "sha256:" ++ String.repeat 64 "a"


hashB : String
hashB =
    "sha256:" ++ String.repeat 64 "b"


fact : String -> String -> Int -> ImportState.FileFact
fact path mime size =
    { name = path |> String.split "/" |> List.reverse |> List.head |> Maybe.withDefault path
    , relativePath = path
    , size = size
    , mime = mime
    , lastModified = 7
    }


step : ImportState.Access -> ImportState.Event -> ImportState.Model -> ( ImportState.Model, List ImportState.Effect, List ImportState.Note )
step =
    ImportState.update


effectLabel : ImportState.Effect -> String
effectLabel effect =
    case effect of
        ImportState.HashEntry _ id ->
            "hash " ++ String.fromInt id

        ImportState.LookupHashes generation hashes ->
            "lookup " ++ String.fromInt generation ++ " " ++ String.fromInt (List.length hashes)

        ImportState.BeginImport id ->
            "begin " ++ String.fromInt id

        ImportState.ReleaseFile id ->
            "release " ++ String.fromInt id

        ImportState.ReleaseFiles ids ->
            "releaseMany " ++ String.fromInt (List.length ids)


labels : List ImportState.Effect -> List String
labels =
    List.map effectLabel


chosen : List ImportState.FileFact -> ( ImportState.Model, List ImportState.Effect, List ImportState.Note )
chosen facts =
    step access (ImportState.FilesChosen 1 facts) ImportState.init


twoReady : ImportState.Model
twoReady =
    let
        ( picked, _, _ ) =
            chosen [ fact "photos/a.jpg" "image/jpeg" 100, fact "photos/b.png" "image/png" 200 ]

        ( afterA, _, _ ) =
            step access (ImportState.HashSucceeded { entryId = 0, generation = 1, hash = hashA, size = 100, lastModified = 7 }) picked

        ( afterB, _, _ ) =
            step access (ImportState.HashSucceeded { entryId = 1, generation = 1, hash = hashB, size = 200, lastModified = 7 }) afterA
    in
    afterB


detail : Post.PostDetail
detail =
    case Decode.decodeString Post.detailDecoder """{"id":"42","preview_url":"/p","original_url":"/o","media_type":"image/jpeg","width":1,"height":1,"source":"s","artist":"a","hash":"h","file_size":1,"created_at":"2026-01-01","tags":[]}""" of
        Ok value ->
            { value | hash = hashA }

        Err _ ->
            -- Unreachable in practice; decoder tests elsewhere fail loudly.
            { id = "42"
            , previewUrl = "/p"
            , originalUrl = "/o"
            , mediaType = "image/jpeg"
            , width = 1
            , height = 1
            , source = "s"
            , artist = "a"
            , hash = hashA
            , fileSize = 1
            , createdAt = ""
            , tags = []
            , tagVersion = 0
            , favorite = False
            , score = 0
            , reactionVersion = 0
            , history = []
            , reactionHistory = []
            , derivativeJobId = Nothing
            , derivativeStatus = Nothing
            , metadataVersion = 0
            , metadataHistory = []
            }


success : Bool -> Maybe String -> Maybe String -> ImportState.ImportOutcome
success duplicate collectionId attachmentError =
    ImportState.ImportSucceeded
        { post = detail
        , duplicate = duplicate
        , collectionId = collectionId
        , attached = collectionId /= Nothing && attachmentError == Nothing
        , attachmentError = attachmentError
        }


receipt : String -> String -> Bool -> Maybe String -> Bool -> Maybe String -> ImportState.ImportOutcome
receipt postId hash duplicate collectionId attached attachmentError =
    ImportState.ImportSucceeded
        { post = { detail | id = postId, hash = hash }
        , duplicate = duplicate
        , collectionId = collectionId
        , attached = attached
        , attachmentError = attachmentError
        }


startedTwo : ImportState.Model
startedTwo =
    step access ImportState.StartRequested twoReady |> (\( m, _, _ ) -> m)


startedWithCollection : ImportState.Model
startedWithCollection =
    let
        ( selected, _, _ ) =
            step access (ImportState.CollectionSelected (Just "c7")) twoReady
    in
    step access ImportState.StartRequested selected |> (\( m, _, _ ) -> m)


isUncertain : ImportState.Model -> Bool
isUncertain model =
    case phaseOf 0 model of
        Just (ImportState.Uncertain _) ->
            True

        _ ->
            False


phaseOf : Int -> ImportState.Model -> Maybe ImportState.Phase
phaseOf entryId model =
    List.filter (\e -> e.id == entryId) model.entries |> List.head |> Maybe.map .phase


caseClassify : Bool
caseClassify =
    ImportState.classify (fact "photos/a.jpg" "image/jpeg" 10) == Ok ()
        && ImportState.classify (fact "photos/b.txt" "text/plain" 10) /= Ok ()
        && ImportState.classify (fact "photos/big.png" "image/png" (26 * 1024 * 1024)) /= Ok ()


caseExcludedKinds : Bool
caseExcludedKinds =
    List.all
        (\path -> ImportState.classify (fact path "image/jpeg" 10) /= Ok ())
        [ ".hidden/a.jpg"
        , "photos/.secret/a.jpg"
        , "photos/.DS_Store" |> (\p -> p)
        , "Library.photoslibrary/originals/a.jpg"
        , "Library.photolibrary/a.jpg"
        , "photos/Thumbs.db"
        , "photos/._a.jpg"
        ]


casePreviewNoWrites : Bool
casePreviewNoWrites =
    let
        ( _, effects, _ ) =
            chosen [ fact "photos/a.jpg" "image/jpeg" 100, fact "notes.txt" "text/plain" 10 ]
    in
    labels effects == [ "releaseMany 1", "hash 0" ]
        && (ImportState.counts (chosen [ fact "photos/a.jpg" "image/jpeg" 100 ] |> (\( m, _, _ ) -> m))).supported == 1


casePreviewCounts : Bool
casePreviewCounts =
    let
        ( model, _, _ ) =
            chosen
                [ fact "photos/a.jpg" "image/jpeg" 100
                , fact "notes.txt" "text/plain" 10
                , fact "photos/big.png" "image/png" (26 * 1024 * 1024)
                , fact ".hidden/c.jpg" "image/jpeg" 10
                ]

        summary =
            ImportState.counts model
    in
    summary.supported == 1 && summary.excluded == 3 && summary.oversize == 1 && summary.unsupported >= 1


caseDefaultSourceAndTags : Bool
caseDefaultSourceAndTags =
    let
        ( drafted, _, _ ) =
            step access (ImportState.TagsEdited "cat, demo") ImportState.init

        ( model, _, _ ) =
            step access (ImportState.FilesChosen 1 [ fact "photos/a.jpg" "image/jpeg" 100 ]) drafted
    in
    case model.entries of
        [ entry ] ->
            entry.source == "folder-import/photos/a.jpg" && entry.tags == [ "cat", "demo" ]

        _ ->
            False


caseRepickReusesIdsWithoutRelease : Bool
caseRepickReusesIdsWithoutRelease =
    let
        ( first, _, _ ) =
            chosen [ fact "a.jpg" "image/jpeg" 100 ]

        ( hashed, _, _ ) =
            step access (ImportState.HashSucceeded { entryId = 0, generation = 1, hash = hashA, size = 100, lastModified = 7 }) first

        ( repicked, effects, _ ) =
            step access (ImportState.FilesChosen 2 [ fact "b.jpg" "image/jpeg" 100 ]) hashed
    in
    labels effects == [ "hash 0" ] && repicked.generation == hashed.generation + 1


caseHashSequentialThenLookup : Bool
caseHashSequentialThenLookup =
    let
        ( picked, firstEffects, _ ) =
            chosen [ fact "a.jpg" "image/jpeg" 100, fact "b.jpg" "image/jpeg" 100 ]

        ( afterA, secondEffects, _ ) =
            step access (ImportState.HashSucceeded { entryId = 0, generation = 1, hash = hashA, size = 100, lastModified = 7 }) picked

        ( _, thirdEffects, _ ) =
            step access (ImportState.HashSucceeded { entryId = 1, generation = 1, hash = hashB, size = 100, lastModified = 7 }) afterA
    in
    labels firstEffects == [ "hash 0" ]
        && labels secondEffects == [ "hash 1" ]
        && labels thirdEffects == [ "lookup 1 2" ]


caseHashChangedSnapshot : Bool
caseHashChangedSnapshot =
    let
        ( picked, _, _ ) =
            chosen [ fact "a.jpg" "image/jpeg" 100 ]

        ( next, _, _ ) =
            step access (ImportState.HashSucceeded { entryId = 0, generation = 1, hash = hashA, size = 999, lastModified = 7 }) picked
    in
    case phaseOf 0 next of
        Just (ImportState.HashFailed _) ->
            True

        _ ->
            False


caseInvalidHash : Bool
caseInvalidHash =
    not (ImportState.isValidHash "sha256:xyz")
        && ImportState.isValidHash hashA
        && (let
                ( picked, _, _ ) =
                    chosen [ fact "a.jpg" "image/jpeg" 100 ]

                ( next, _, _ ) =
                    step access (ImportState.HashSucceeded { entryId = 0, generation = 1, hash = "SHA256:bad", size = 100, lastModified = 7 }) picked
            in
            case phaseOf 0 next of
                Just (ImportState.HashFailed _) ->
                    True

                _ ->
                    False
           )


caseStaleDuplicatesIgnored : Bool
caseStaleDuplicatesIgnored =
    let
        ( stale, _, _ ) =
            step access (ImportState.DuplicatesArrived { generation = 99, matches = [ { hash = hashA, postId = "9" } ] }) twoReady

        ( current, _, _ ) =
            step access (ImportState.DuplicatesArrived { generation = stale.generation, matches = [ { hash = hashA, postId = "9" } ] }) stale
    in
    (List.head stale.entries |> Maybe.andThen .duplicatePostId) == Nothing
        && (List.head current.entries |> Maybe.andThen .duplicatePostId) == Just "9"


caseStartLocked : Bool
caseStartLocked =
    let
        ( next, effects, _ ) =
            step lockedAccess ImportState.StartRequested twoReady
    in
    effects == [] && next.status == Just "Unlock to start the import." && not next.started


caseStartWhileHashing : Bool
caseStartWhileHashing =
    let
        ( picked, _, _ ) =
            chosen [ fact "a.jpg" "image/jpeg" 100 ]

        ( next, effects, _ ) =
            step access ImportState.StartRequested picked
    in
    effects == [] && not next.started


caseKnownDuplicateWithCollectionSchedulesWrite : Bool
caseKnownDuplicateWithCollectionSchedulesWrite =
    let
        ( marked, _, _ ) =
            step access (ImportState.DuplicatesArrived { generation = twoReady.generation, matches = [ { hash = hashA, postId = "9" } ] }) twoReady

        ( selected, _, _ ) =
            step access (ImportState.CollectionSelected (Just "c7")) marked

        ( started, effects, _ ) =
            step access ImportState.StartRequested selected

        ( settled, settleEffects, notes ) =
            step access
                (ImportState.ImportSettled { entryId = 0, generation = 1, outcome = receipt "9" hashA True (Just "c7") True Nothing })
                started
    in
    labels effects == [ "begin 0" ]
        && phaseOf 0 started == Just (ImportState.Importing 1)
        && phaseOf 0 settled == Just (ImportState.Skipped "9")
        && labels settleEffects == [ "release 0", "begin 1" ]
        && List.length notes == 1


caseKnownDuplicateRevalidatesWithoutCollection : Bool
caseKnownDuplicateRevalidatesWithoutCollection =
    let
        ( marked, _, _ ) =
            step access (ImportState.DuplicatesArrived { generation = twoReady.generation, matches = [ { hash = hashA, postId = "9" } ] }) twoReady

        ( started, effects, _ ) =
            step access ImportState.StartRequested marked

        ( settled, _, _ ) =
            step access
                (ImportState.ImportSettled { entryId = 0, generation = 1, outcome = success True Nothing Nothing })
                started
    in
    labels effects == [ "begin 0" ] && phaseOf 0 settled == Just (ImportState.Skipped "42")


caseErrorKnownDuplicateUploads : Bool
caseErrorKnownDuplicateUploads =
    let
        ( withPolicy, _, _ ) =
            step access (ImportState.PolicySelected ImportState.ErrorOnDuplicates) twoReady

        ( marked, _, _ ) =
            step access (ImportState.DuplicatesArrived { generation = withPolicy.generation, matches = [ { hash = hashA, postId = "9" } ] }) withPolicy

        ( started, effects, _ ) =
            step access ImportState.StartRequested marked

        ( settled, _, _ ) =
            step access
                (ImportState.ImportSettled
                    { entryId = 0
                    , generation = 1
                    , outcome = ImportState.ImportFailed { message = "conflict", authRejected = False, authStatus = 0 }
                    }
                )
                started
    in
    labels effects == [ "begin 0" ]
        && (case phaseOf 0 settled of
                Just (ImportState.Failed _) ->
                    True

                _ ->
                    False
           )


caseCapturedCollection : Bool
caseCapturedCollection =
    let
        ( selected, _, _ ) =
            step access (ImportState.CollectionSelected (Just "c7")) twoReady

        ( started, _, _ ) =
            step access ImportState.StartRequested selected

        ( changed, _, _ ) =
            step access (ImportState.CollectionSelected (Just "other")) started
    in
    started.capturedCollectionId == Just "c7" && changed.capturedCollectionId == Just "c7"


caseSequentialImportAndCounts : Bool
caseSequentialImportAndCounts =
    let
        ( settled, effects, notes ) =
            step access
                (ImportState.ImportSettled { entryId = 0, generation = 1, outcome = success False Nothing Nothing })
                startedTwo
    in
    labels effects == [ "release 0", "begin 1" ]
        && List.length notes == 1
        && (ImportState.counts settled).imported == 1


caseServerSkip : Bool
caseServerSkip =
    let
        ( settled, _, _ ) =
            step access
                (ImportState.ImportSettled { entryId = 0, generation = 1, outcome = success True Nothing Nothing })
                startedTwo
    in
    phaseOf 0 settled == Just (ImportState.Skipped "42")


caseAttachFailureNotReuploaded : Bool
caseAttachFailureNotReuploaded =
    let
        ( settled, _, _ ) =
            step access
                (ImportState.ImportSettled { entryId = 0, generation = 1, outcome = receipt "42" hashA False (Just "c7") False (Just "collection changed") })
                startedWithCollection

        ( resumed, effects, _ ) =
            ImportState.resume access settled
    in
    (case phaseOf 0 settled of
        Just (ImportState.Imported info) ->
            info.attachmentError == Just "collection changed" && not info.attached

        _ ->
            False
    )
        && not (List.member "begin 0" (labels effects))
        && (ImportState.counts settled).imported == 1


caseUncertainNoRetry : Bool
caseUncertainNoRetry =
    let
        ( settled, effects, _ ) =
            step access
                (ImportState.ImportSettled { entryId = 0, generation = 1, outcome = ImportState.ImportUncertain "lost" })
                startedTwo

        ( resumed, resumeEffects, _ ) =
            ImportState.resume access settled
    in
    (case phaseOf 0 settled of
        Just (ImportState.Uncertain _) ->
            True

        _ ->
            False
    )
        && labels effects == [ "release 0", "begin 1" ]
        && not (List.member "begin 0" (labels resumeEffects))
        && (ImportState.counts resumed).uncertain == 1


caseCapturedSkipPolicyIgnoresInFlightEdit : Bool
caseCapturedSkipPolicyIgnoresInFlightEdit =
    let
        ( edited, _, _ ) =
            step access (ImportState.PolicySelected ImportState.ErrorOnDuplicates) startedTwo

        ( settled, _, notes ) =
            step access
                (ImportState.ImportSettled { entryId = 0, generation = 1, outcome = success True Nothing Nothing })
                edited
    in
    edited.capturedDuplicatePolicy == ImportState.SkipDuplicates
        && phaseOf 0 settled == Just (ImportState.Skipped "42")
        && List.length notes == 1


caseCapturedErrorPolicyDuplicateIsUncertain : Bool
caseCapturedErrorPolicyDuplicateIsUncertain =
    let
        ( withPolicy, _, _ ) =
            step access (ImportState.PolicySelected ImportState.ErrorOnDuplicates) twoReady

        ( started, _, _ ) =
            step access ImportState.StartRequested withPolicy

        ( edited, _, _ ) =
            step access (ImportState.PolicySelected ImportState.SkipDuplicates) started

        ( settled, _, notes ) =
            step access
                (ImportState.ImportSettled { entryId = 0, generation = 1, outcome = success True Nothing Nothing })
                edited
    in
    edited.capturedDuplicatePolicy == ImportState.ErrorOnDuplicates
        && isUncertain settled
        && notes == []


caseStaleAuthDoesNotHold : Bool
caseStaleAuthDoesNotHold =
    let
        renewed =
            { writesEnabled = True, accessGeneration = 2 }

        ( settled, effects, notes ) =
            step renewed
                (ImportState.ImportSettled
                    { entryId = 0
                    , generation = 1
                    , outcome = ImportState.ImportFailed { message = "no", authRejected = True, authStatus = 401 }
                    }
                )
                startedTwo
    in
    not settled.held
        && (case phaseOf 0 settled of
                Just (ImportState.Failed _) ->
                    True

                _ ->
                    False
           )
        && phaseOf 1 settled == Just (ImportState.Importing 2)
        && labels effects == [ "release 0", "begin 1" ]
        && notes == [ ImportState.CredentialRejected 1 401 ]


caseAuthHoldsLater : Bool
caseAuthHoldsLater =
    let
        ( settled, effects, notes ) =
            step access
                (ImportState.ImportSettled
                    { entryId = 0
                    , generation = 1
                    , outcome = ImportState.ImportFailed { message = "no", authRejected = True, authStatus = 401 }
                    }
                )
                startedTwo

        ( resumed, resumeEffects, _ ) =
            ImportState.resume { writesEnabled = True, accessGeneration = 2 } settled
    in
    settled.held
        && phaseOf 1 settled == Just ImportState.QueuedImport
        && labels effects == [ "release 0" ]
        && notes == [ ImportState.CredentialRejected 1 401 ]
        && labels resumeEffects == [ "begin 1" ]


caseLateAckSettles : Bool
caseLateAckSettles =
    let
        renewed =
            { writesEnabled = False, accessGeneration = 9 }

        ( settled, _, notes ) =
            step renewed
                (ImportState.ImportSettled { entryId = 0, generation = 1, outcome = success False Nothing Nothing })
                startedTwo
    in
    (case phaseOf 0 settled of
        Just (ImportState.Imported _) ->
            True

        _ ->
            False
    )
        && List.length notes == 1


caseWrongGenerationIgnored : Bool
caseWrongGenerationIgnored =
    let
        ( settled, effects, notes ) =
            step access
                (ImportState.ImportSettled { entryId = 0, generation = 99, outcome = success False Nothing Nothing })
                startedTwo
    in
    effects == [] && notes == [] && phaseOf 0 settled == Just (ImportState.Importing 1)


caseMaxSupported : Bool
caseMaxSupported =
    let
        facts =
            List.range 1 1001 |> List.map (\i -> fact ("p/" ++ String.fromInt i ++ ".jpg") "image/jpeg" 10)

        ( model, _, _ ) =
            chosen facts

        summary =
            ImportState.counts model
    in
    summary.supported == 1000 && model.overflow == 1 && String.contains "1000" (model.status |> Maybe.withDefault "")


caseBatchAndUrl : Bool
caseBatchAndUrl =
    (ImportApi.batchHashes (List.range 1 205 |> List.map String.fromInt)
        |> List.map List.length
        |> (==) [ 100, 100, 5 ]
    )
        && String.contains "hashes=sha256%3Aabc" (ImportApi.duplicatesUrl "" [ "sha256:abc" ])


caseDuplicateDecoder : Bool
caseDuplicateDecoder =
    Decode.decodeString ImportApi.duplicatesDecoder """{"matches":[{"hash":"sha256:a","post_id":"9"}]}"""
        == Ok [ { hash = "sha256:a", postId = "9" } ]


caseImportResultDecoder : Bool
caseImportResultDecoder =
    case Decode.decodeString ImportApi.importResultDecoder """{"post":{"id":"42","preview_url":"/p","original_url":"/o","media_type":"image/jpeg","width":1,"height":1,"source":"s","artist":"a","hash":"h","file_size":1,"created_at":"2026-01-01","tags":[]},"duplicate":true,"collection_id":null,"attached":false,"attachment_error":"nope"}""" of
        Ok result ->
            result.duplicate && result.collectionId == Nothing && result.attachmentError == Just "nope" && result.post.id == "42"

        Err _ ->
            False


settleFirst : ImportState.Model -> ImportState.ImportOutcome -> ( ImportState.Model, List ImportState.Effect, List ImportState.Note )
settleFirst model outcome =
    step access (ImportState.ImportSettled { entryId = 0, generation = 1, outcome = outcome }) model


caseReceiptWrongHashUncertain : Bool
caseReceiptWrongHashUncertain =
    let
        ( settled, effects, notes ) =
            settleFirst startedTwo (receipt "42" hashB False Nothing False Nothing)
    in
    isUncertain settled && notes == [] && labels effects == [ "release 0", "begin 1" ] && (ImportState.counts settled).uncertain == 1


caseReceiptBadPostIdsUncertain : Bool
caseReceiptBadPostIdsUncertain =
    List.all
        (\postId ->
            settleFirst startedTwo (receipt postId hashA False Nothing False Nothing) |> (\( m, _, notes ) -> isUncertain m && notes == [])
        )
        [ "0", "-3", "042", "+42", " 42", "abc", "" ]


caseReceiptWrongCollectionUncertain : Bool
caseReceiptWrongCollectionUncertain =
    (settleFirst startedTwo (receipt "42" hashA False (Just "c7") True Nothing) |> (\( m, _, notes ) -> isUncertain m && notes == []))
        && (settleFirst startedWithCollection (receipt "42" hashA False Nothing False Nothing) |> (\( m, _, notes ) -> isUncertain m && notes == []))
        && (settleFirst startedWithCollection (receipt "42" hashA False (Just "other") True Nothing) |> (\( m, _, notes ) -> isUncertain m && notes == []))


caseReceiptAttachedRules : Bool
caseReceiptAttachedRules =
    (settleFirst startedTwo (receipt "42" hashA False Nothing True Nothing) |> (\( m, _, notes ) -> isUncertain m && notes == []))
        && (settleFirst startedWithCollection (receipt "42" hashA False (Just "c7") False Nothing) |> (\( m, _, notes ) -> isUncertain m && notes == []))
        && (settleFirst startedWithCollection (receipt "42" hashA False (Just "c7") False (Just "   ")) |> (\( m, _, notes ) -> isUncertain m && notes == []))
        && (settleFirst startedWithCollection (receipt "42" hashA False (Just "c7") True (Just "boom")) |> (\( m, _, notes ) -> isUncertain m && notes == []))


caseReceiptTargetAttachedValid : Bool
caseReceiptTargetAttachedValid =
    let
        ( settled, _, notes ) =
            settleFirst startedWithCollection (receipt "42" hashA False (Just "c7") True Nothing)
    in
    (case phaseOf 0 settled of
        Just (ImportState.Imported info) ->
            info.attached && info.postId == "42"

        _ ->
            False
    )
        && List.length notes == 1


caseReceiptDuplicateExistingIdValid : Bool
caseReceiptDuplicateExistingIdValid =
    let
        ( settled, _, notes ) =
            settleFirst startedTwo (receipt "9" hashA True Nothing False Nothing)
    in
    phaseOf 0 settled == Just (ImportState.Skipped "9") && List.length notes == 1


caseCanonicalPostId : Bool
caseCanonicalPostId =
    ImportState.isCanonicalPostId "42" && not (ImportState.isCanonicalPostId "042") && not (ImportState.isCanonicalPostId "0")


casePickBlockedWhileHashing : Bool
casePickBlockedWhileHashing =
    let
        ( picked, _, _ ) =
            chosen [ fact "a.jpg" "image/jpeg" 100 ]

        ( repicked, effects, _ ) =
            step access (ImportState.FilesChosen 2 [ fact "b.jpg" "image/jpeg" 100 ]) picked
    in
    List.map .name repicked.entries == [ "a.jpg" ] && effects == [] && ImportState.isPickBlocked picked && not (ImportState.isPickBlocked twoReady)


caseStaleHashGenerationIgnored : Bool
caseStaleHashGenerationIgnored =
    let
        ( first, _, _ ) =
            chosen [ fact "a.jpg" "image/jpeg" 100 ]

        ( hashed, _, _ ) =
            step access (ImportState.HashSucceeded { entryId = 0, generation = 1, hash = hashA, size = 100, lastModified = 7 }) first

        ( repicked, _, _ ) =
            step access (ImportState.FilesChosen 2 [ fact "b.jpg" "image/jpeg" 100 ]) hashed

        ( stale, effects, _ ) =
            step access (ImportState.HashSucceeded { entryId = 0, generation = 1, hash = hashB, size = 100, lastModified = 7 }) repicked
    in
    repicked.generation == 2
        && effects == []
        && (case phaseOf 0 stale of
                Just ImportState.Hashing ->
                    True

                _ ->
                    False
           )
        && (List.head stale.entries |> Maybe.andThen .hash) == Nothing


caseCancelledPickerGeneration : Bool
caseCancelledPickerGeneration =
    let
        ( picked, _, _ ) =
            step access (ImportState.FilesChosen 2 [ fact "after-cancel.png" "image/png" 100 ]) ImportState.init

        ( hashed, _, _ ) =
            step access (ImportState.HashSucceeded { entryId = 0, generation = 2, hash = hashA, size = 100, lastModified = 7 }) picked
    in
    hashed.generation == 2 && phaseOf 0 hashed == Just ImportState.Hashed


caseIdenticalFactsCap : Bool
caseIdenticalFactsCap =
    let
        ( picked, effects, _ ) =
            chosen (List.repeat 1001 (fact "same.png" "image/png" 100))
    in
    (ImportState.counts picked).supported == 1000
        && picked.overflow == 1
        && labels effects == [ "releaseMany 1", "hash 0" ]


caseSettingsCapturedAtStart : Bool
caseSettingsCapturedAtStart =
    let
        ( tags, _, _ ) =
            step access (ImportState.TagsEdited "reference, final") twoReady

        ( artist, _, _ ) =
            step access (ImportState.ArtistEdited "Preview artist") tags

        ( source, _, _ ) =
            step access (ImportState.SourcePrefixEdited "archive") artist

        ( started, _, _ ) =
            step access ImportState.StartRequested source

        ( edited, _, _ ) =
            step access (ImportState.ArtistEdited "Next import") started
    in
    case List.head edited.entries of
        Just entry ->
            entry.tags == [ "reference", "final" ]
                && entry.artist == "Preview artist"
                && entry.source == "archive/photos/a.jpg"

        Nothing ->
            False


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "import classify supported and bounds", caseClassify )
    , ( "import excludes hidden metadata and packages", caseExcludedKinds )
    , ( "import preview performs no writes before confirm", casePreviewNoWrites )
    , ( "import preview counts unsupported and oversize", casePreviewCounts )
    , ( "import default source prefix and shared tags", caseDefaultSourceAndTags )
    , ( "import repick reuses ids without releasing new files", caseRepickReusesIdsWithoutRelease )
    , ( "import hashing is sequential then batched lookup", caseHashSequentialThenLookup )
    , ( "import changed file after hash fails snapshot", caseHashChangedSnapshot )
    , ( "import invalid hash rejected", caseInvalidHash )
    , ( "import stale duplicate lookup ignored", caseStaleDuplicatesIgnored )
    , ( "import start requires unlock", caseStartLocked )
    , ( "import start waits for hashing", caseStartWhileHashing )
    , ( "import known duplicate with collection schedules write and confirmed attached skip", caseKnownDuplicateWithCollectionSchedulesWrite )
    , ( "import known duplicate revalidates through server without collection", caseKnownDuplicateRevalidatesWithoutCollection )
    , ( "import error policy uploads and settles failed", caseErrorKnownDuplicateUploads )
    , ( "import captured collection survives later selection", caseCapturedCollection )
    , ( "import uploads sequentially and counts imported", caseSequentialImportAndCounts )
    , ( "import server duplicate counts skipped", caseServerSkip )
    , ( "import attachment failure keeps durable post without reupload", caseAttachFailureNotReuploaded )
    , ( "import uncertain response is never auto-resubmitted", caseUncertainNoRetry )
    , ( "import 401 holds later files until resume", caseAuthHoldsLater )
    , ( "import stale 401 after unlock does not hold later files", caseStaleAuthDoesNotHold )
    , ( "import captured skip policy ignores in-flight option edit", caseCapturedSkipPolicyIgnoresInFlightEdit )
    , ( "import captured error policy duplicate receipt is uncertain", caseCapturedErrorPolicyDuplicateIsUncertain )
    , ( "import late durable ack settles captured entry", caseLateAckSettles )
    , ( "import wrong generation settle ignored", caseWrongGenerationIgnored )
    , ( "import cancelled picker and next accepted generation align", caseCancelledPickerGeneration )
    , ( "import identical file facts cannot bypass cap", caseIdenticalFactsCap )
    , ( "import edited preview settings captured at start", caseSettingsCapturedAtStart )
    , ( "import folder capped at 1000 supported entries", caseMaxSupported )
    , ( "import duplicate hashes batch at 100 and encode url", caseBatchAndUrl )
    , ( "import duplicates decoder", caseDuplicateDecoder )
    , ( "import result decoder reads attachment error", caseImportResultDecoder )
    , ( "import receipt wrong hash is uncertain without confirmation", caseReceiptWrongHashUncertain )
    , ( "import receipt non-canonical post ids are uncertain", caseReceiptBadPostIdsUncertain )
    , ( "import receipt wrong collection is uncertain", caseReceiptWrongCollectionUncertain )
    , ( "import receipt attached rules are enforced", caseReceiptAttachedRules )
    , ( "import receipt target attached is confirmed", caseReceiptTargetAttachedValid )
    , ( "import receipt hash-match duplicate with existing id is valid", caseReceiptDuplicateExistingIdValid )
    , ( "import canonical post id helper", caseCanonicalPostId )
    , ( "import pick blocked while hashing", casePickBlockedWhileHashing )
    , ( "import stale hash generation ignored after repick", caseStaleHashGenerationIgnored )
    ]
