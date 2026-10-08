module Feature.Inspector exposing (Config, view)

import Api.Post
import Domain.Post exposing (PostDetail, PostSummary, TagRevision)
import Feature.Selection
import Html exposing (Html, a, aside, button, dd, div, dl, dt, input, li, p, span, text, ul)
import Html.Attributes exposing (attribute, class, classList, disabled, href, placeholder, rel, style, target, title, type_, value)
import Html.Events exposing (on, onClick, onInput)
import Json.Decode as Decode
import Set exposing (Set)
import Ui.Media
import Ui.Panel as Panel exposing (Presentation)


type alias Config msg =
    { apiBase : String
    , presentation : Presentation
    , active : Maybe PostSummary
    , detail : Maybe PostDetail
    , detailLoading : Bool
    , detailError : Maybe String
    , selectedCount : Int
    , commonTags : List String
    , missing : Set String
    , onTag : String -> Bool -> msg
    , onClear : msg
    , onClose : msg
    , onApiPending : String -> msg
    , onFavorite : msg
    , onAddToCollection : msg
    , onMediaError : String -> msg
    , tagAdd : String
    , tagRemove : String
    , tagStatus : Maybe String
    , tagSaving : Bool
    , onTagAdd : String -> msg
    , onTagRemove : String -> msg
    , onSaveTags : msg
    , scoreDraft : String
    , reactionSaving : Bool
    , reactionStatus : Maybe String
    , onScoreDraft : String -> msg
    , onSaveScore : msg
    , onRevert : Int -> msg
    , onConfirmRevert : Int -> msg
    , onCancelRevert : msg
    , revertConfirm : Maybe Int
    , revertPending : Maybe Int
    , revertStatus : Maybe String
    }


view : Config msg -> Html msg
view config =
    aside [ class (Panel.presentationClass "inspector" config.presentation), attribute "aria-label" "Inspector" ]
        [ Panel.header "Inspector" config.presentation config.onClose
        , div [ class "panel-body" ]
            (case config.active of
                Nothing ->
                    [ p [ class "panel-note" ] [ text "No active post. Click a thumbnail or use the arrow keys." ]
                    , selectionSection config
                    , if config.selectedCount > 0 then
                        tagEditor config

                      else
                        text ""
                    ]

                Just post ->
                    [ identitySection config post
                    , selectionSection config
                    , detailSection config post
                    , reactionSection config post
                    , Panel.section "Tags" (tagList config (detailTags config post))
                    , tagEditor config
                    , Panel.section "History" (historyList config)
                    ]
            )
        ]


detailSection : Config msg -> PostSummary -> Html msg
detailSection config post =
    case config.detail of
        Just detail ->
            if detail.id == post.id then
                Panel.section "Source"
                    [ dl [ class "facts" ]
                        [ fact "Source" (if detail.source == "" then "—" else detail.source)
                        , fact "Artist" (if detail.artist == "" then "—" else detail.artist)
                        , fact "Hash" (if detail.hash == "" then "—" else detail.hash)
                        , fact "File size" (if detail.fileSize <= 0 then "—" else String.fromInt detail.fileSize ++ " bytes")
                        , fact "Created" (if detail.createdAt == "" then "—" else detail.createdAt)
                        ]
                    ]

            else
                detailPlaceholder config

        Nothing ->
            detailPlaceholder config


detailPlaceholder : Config msg -> Html msg
detailPlaceholder config =
    Panel.section "Source"
        (if config.detailLoading then
            [ p [ class "panel-note" ] [ text "Loading post details…" ] ]

         else
            case config.detailError of
                Just message ->
                    [ p [ class "panel-note status-error" ] [ text ("Post details unavailable: " ++ message) ] ]

                Nothing ->
                    [ p [ class "panel-note" ] [ text "Select a post to load its details." ] ]
        )


detailTags : Config msg -> PostSummary -> List String
detailTags config post =
    case config.detail of
        Just detail ->
            if detail.id == post.id then
                detail.tags

            else
                post.tags

        Nothing ->
            post.tags


identitySection : Config msg -> PostSummary -> Html msg
identitySection config post =
    let
        ratio =
            if post.height > 0 then
                toFloat post.width / toFloat post.height

            else
                1
    in
    div [ class "panel-section inspector-identity" ]
        [ div [ class "inspector-preview", style "aspect-ratio" (String.fromFloat ratio) ]
            [ Ui.Media.image [ class "inspector-preview-image" ]
                { url = Api.Post.mediaUrl config.apiBase post.previewUrl
                , postId = post.id
                , missing = Set.member post.id config.missing
                , onError = config.onMediaError
                }
            ]
        , p [ class "inspector-id" ] [ text ("Post #" ++ post.id) ]
        , dl [ class "facts" ]
            [ fact "Dimensions" (String.fromInt post.width ++ " × " ++ String.fromInt post.height)
            , fact "Aspect" (aspect post)
            , fact "Media type" post.mediaType
            , div [ class "fact" ]
                [ dt [] [ text "Original" ]
                , dd [] [ a [ href (Api.Post.mediaUrl config.apiBase post.originalUrl), target "_blank", rel "noopener" ] [ text "Open" ] ]
                ]
            ]
        ]


selectionSection : Config msg -> Html msg
selectionSection config =
    Panel.section "Selection"
        ([ Feature.Selection.view
            { count = config.selectedCount
            , activeId = Maybe.map .id config.active
            , onClear = config.onClear
            , onApiPending = config.onApiPending
            , onFavorite = config.onFavorite
            , onAddToCollection = config.onAddToCollection
            }
         ]
            ++ (if config.selectedCount > 1 then
                    [ p [ class "panel-label" ] [ text "Common tags" ]
                    , if List.isEmpty config.commonTags then
                        p [ class "panel-note" ] [ text "None in common." ]

                      else
                        ul [ class "tag-list" ] (List.map (tagItem config) config.commonTags)
                    ]

                else
                    []
               )
        )


tagList : Config msg -> List String -> List (Html msg)
tagList config tags =
    if List.isEmpty tags then
        [ p [ class "panel-note" ] [ text "No tags available." ] ]

    else
        [ ul [ class "tag-list" ] (List.map (tagItem config) tags)
        , p [ class "panel-note" ] [ text "Click to filter by a tag; Alt+click to exclude it." ]
        ]


tagEditor : Config msg -> Html msg
tagEditor config =
    Panel.section "Edit tags"
        [ input
            [ class "query-input"
            , type_ "text"
            , placeholder "Add tag"
            , value config.tagAdd
            , onInput config.onTagAdd
            , attribute "aria-label" "Tags to add"
            , attribute "autocomplete" "off"
            ]
            []
        , input
            [ class "query-input"
            , type_ "text"
            , placeholder "Remove tag"
            , value config.tagRemove
            , onInput config.onTagRemove
            , attribute "aria-label" "Tags to remove"
            , attribute "autocomplete" "off"
            ]
            []
        , button
            [ class "button"
            , type_ "button"
            , onClick config.onSaveTags
            , disabled (config.tagSaving || (String.trim config.tagAdd == "" && String.trim config.tagRemove == ""))
            ]
            [ text "Save tags" ]
        , case config.tagStatus of
            Just status ->
                p [ class "panel-note status-error" ] [ text status ]

            Nothing ->
                p [ class "panel-note" ] [ text "Separate multiple tags with commas." ]
        ]


reactionSection : Config msg -> PostSummary -> Html msg
reactionSection config post =
    case config.detail of
        Just detail ->
            if detail.id == post.id then
                Panel.section "Rating"
                    [ button
                        [ class "button"
                        , type_ "button"
                        , onClick config.onFavorite
                        , disabled config.reactionSaving
                        ]
                        [ text
                            (if detail.favorite then
                                "★ Favorite"

                             else
                                "☆ Favorite"
                            )
                        ]
                    , input
                        [ class "query-input"
                        , type_ "number"
                        , attribute "min" "0"
                        , value config.scoreDraft
                        , onInput config.onScoreDraft
                        , attribute "aria-label" "Score"
                        , disabled config.reactionSaving
                        ]
                        []
                    , button
                        [ class "button"
                        , type_ "button"
                        , onClick config.onSaveScore
                        , disabled config.reactionSaving
                        ]
                        [ text "Save score" ]
                    , case config.reactionStatus of
                        Just status ->
                            p [ class "panel-note status-error" ] [ text status ]

                        Nothing ->
                            p [ class "panel-note" ] [ text ("Score " ++ String.fromInt detail.score) ]
                    ]

            else
                Panel.section "Rating" [ p [ class "panel-note" ] [ text "Select a post to edit its rating." ] ]

        Nothing ->
            Panel.section "Rating" [ p [ class "panel-note" ] [ text "Loading rating…" ] ]


historyList : Config msg -> List (Html msg)
historyList config =
    case config.detail of
        Just detail ->
            if List.isEmpty detail.history then
                [ p [ class "panel-note" ] [ text "No tag revisions yet." ] ]

            else
                [ ul [ class "history-list" ] (List.map (historyItem config) detail.history)
                , case config.revertStatus of
                    Just status ->
                        p [ class "panel-note status-info" ] [ text status ]

                    Nothing ->
                        text ""
                , case config.revertPending of
                    Just v ->
                        p [ class "panel-note" ] [ text ("Reverting to v" ++ String.fromInt v ++ "…") ]

                    Nothing ->
                        text ""
                ]

        Nothing ->
            [ p [ class "panel-note" ] [ text "Select a post to load history." ] ]


historyItem : Config msg -> TagRevision -> Html msg
historyItem config revision =
    let
        isConfirming =
            config.revertConfirm == Just revision.version

        isPending =
            config.revertPending == Just revision.version

        canRevert =
            revision.revertible

        beforeTags =
            revision.removedTags

        afterTags =
            revision.addedTags

        targetTags =
            revision.targetTags
    in
    li [ class "history-item", classList [ ( "is-legacy", not canRevert ), ( "is-pending", isPending ) ] ]
        [ div [ class "history-header" ]
            [ span [ class "history-version" ] [ text ("v" ++ String.fromInt revision.version) ]
            , span [ class "history-kind" ] [ text revision.kind ]
            , span [ class "history-time" ] [ text revision.createdAt ]
            , if not canRevert then
                span [ class "history-legacy", title "Legacy revision without stored tag state" ] [ text "legacy" ]

              else
                text ""
            ]
        , div [ class "history-diff" ]
            [ div [ class "history-chips before" ]
                (if List.isEmpty beforeTags then
                    [ span [ class "panel-note" ] [ text "No removed tags" ] ]

                 else
                    [ span [ class "history-label" ] [ text "Removed:" ]
                    , ul [ class "tag-list" ] (List.map chipRemoved beforeTags)
                    ]
                )
            , div [ class "history-chips after" ]
                (if List.isEmpty afterTags then
                    [ span [ class "panel-note" ] [ text "No added tags" ] ]

                 else
                    [ span [ class "history-label" ] [ text "Added:" ]
                    , ul [ class "tag-list" ] (List.map chipAdded afterTags)
                    ]
                )
            , div [ class "history-chips result" ]
                (case targetTags of
                    Nothing ->
                        [ p [ class "panel-note status-error" ] [ text "No stored tag state — cannot revert" ] ]

                    Just tags ->
                        if List.isEmpty tags then
                            [ span [ class "history-label" ] [ text "Result:" ]
                            , span [ class "panel-note" ] [ text "No tags" ]
                            ]

                        else
                            [ span [ class "history-label" ] [ text "Result:" ]
                            , ul [ class "tag-list" ] (List.map chipResult tags)
                            ]
                )
            ]
        , div [ class "history-actions" ]
            (if not canRevert then
                [ button
                    [ class "button"
                    , type_ "button"
                    , disabled True
                    , title "Legacy revision cannot be reverted"
                    ]
                    [ text "Revert" ]
                ]

             else if isPending then
                [ button
                    [ class "button is-pending"
                    , type_ "button"
                    , disabled True
                    ]
                    [ text "Reverting…" ]
                ]

             else if isConfirming then
                [ p [ class "panel-note" ] [ text ("Revert to v" ++ String.fromInt revision.version ++ "?") ]
                , button
                    [ class "button"
                    , type_ "button"
                    , onClick (config.onConfirmRevert revision.version)
                    , disabled config.tagSaving
                    ]
                    [ text "Confirm" ]
                , button
                    [ class "button button-quiet"
                    , type_ "button"
                    , onClick config.onCancelRevert
                    , disabled config.tagSaving
                    ]
                    [ text "Cancel" ]
                ]

             else
                [ button
                    [ class "button"
                    , type_ "button"
                    , onClick (config.onRevert revision.version)
                    , disabled (config.tagSaving || config.revertPending /= Nothing)
                    ]
                    [ text "Revert" ]
                ]
            )
        ]


chipAdded : String -> Html msg
chipAdded tag =
    li [] [ span [ class "chip chip-added", title ("Added " ++ tag) ] [ text tag ] ]


chipRemoved : String -> Html msg
chipRemoved tag =
    li [] [ span [ class "chip chip-removed", title ("Removed " ++ tag) ] [ text tag ] ]


chipResult : String -> Html msg
chipResult tag =
    li [] [ span [ class "chip chip-result", title tag ] [ text tag ] ]


tagItem : Config msg -> String -> Html msg
tagItem config tag =
    li []
        [ button
            [ class "chip"
            , type_ "button"
            , title ("Filter by " ++ tag ++ " (Alt+click to exclude)")
            , on "click" (Decode.map (config.onTag tag) (Decode.field "altKey" Decode.bool))
            ]
            [ text tag ]
        ]


fact : String -> String -> Html msg
fact label value =
    div [ class "fact" ] [ dt [] [ text label ], dd [] [ text value ] ]


aspect : PostSummary -> String
aspect post =
    let
        hundredths =
            if post.height > 0 then
                round (toFloat post.width / toFloat post.height * 100)

            else
                100

        frac =
            modBy 100 hundredths
    in
    String.fromInt (hundredths // 100)
        ++ "."
        ++ (if frac < 10 then
                "0"

            else
                ""
           )
        ++ String.fromInt frac
        ++ " : 1"
